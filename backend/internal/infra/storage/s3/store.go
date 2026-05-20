// Package s3 is the S3 driver for the storage port.
package s3

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

// Store wraps an s3.Client + PresignClient bound to a single bucket.
type Store struct {
	client      *s3.Client
	presign     *s3.PresignClient
	bucket      string
	presignHost *url.URL // overrides scheme+host of presigned URLs when set
}

// New constructs the driver. publicEndpoint, when non-empty, overrides the
// scheme+host of presigned URLs (used to expose LocalStack via localhost from
// inside a Docker network). Empty is correct for real AWS.
func New(c *s3.Client, bucket, publicEndpoint string) (*Store, error) {
	s := &Store{client: c, presign: s3.NewPresignClient(c), bucket: bucket}
	if publicEndpoint != "" {
		u, err := url.Parse(publicEndpoint)
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("s3.New: invalid public endpoint %q: %w", publicEndpoint, err)
		}
		s.presignHost = u
	}
	return s, nil
}

// rewritePresignURL swaps the SDK-emitted host for the configured public host.
// No-op when presignHost is unset (production) or input is unparseable.
func (s *Store) rewritePresignURL(raw string) string {
	if s.presignHost == nil {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = s.presignHost.Scheme
	u.Host = s.presignHost.Host
	return u.String()
}

// Bucket exposes the configured bucket name.
func (s *Store) Bucket() string { return s.bucket }

func (s *Store) Put(ctx context.Context, in storage.PutInput) (storage.PutOutput, error) {
	if in.Key == "" {
		return storage.PutOutput{}, errors.New("s3.Put: key required")
	}
	body, sum, size, err := materialize(in.Body, in.SHA256Hex, in.SizeBytes)
	if err != nil {
		return storage.PutOutput{}, err
	}
	po := &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           aws.String(in.Key),
		Body:          body,
		ContentLength: aws.Int64(size),
	}
	if in.ContentType != "" {
		po.ContentType = aws.String(in.ContentType)
	}
	if len(in.Metadata) > 0 {
		po.Metadata = in.Metadata
	}
	if len(in.Tags) > 0 {
		po.Tagging = aws.String(encodeTags(in.Tags))
	}
	resp, err := s.client.PutObject(ctx, po)
	if err != nil {
		return storage.PutOutput{}, fmt.Errorf("s3.PutObject: %w", err)
	}
	etag := ""
	if resp.ETag != nil {
		etag = *resp.ETag
	}
	return storage.PutOutput{Key: in.Key, SHA256Hex: sum, SizeBytes: size, ETag: etag}, nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3.GetObject: %w", err)
	}
	return out.Body, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	return err
}

func (s *Store) PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error) {
	in := &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}
	// Override the response Content-Type + Content-Disposition based on the
	// key's extension so the browser plays/displays the asset inline rather
	// than downloading it as application/octet-stream. Objects uploaded
	// without a Content-Type otherwise present as a generic blob, which
	// browser previews cannot render inline.
	if ct := contentTypeFromKey(key); ct != "" {
		in.ResponseContentType = aws.String(ct)
		in.ResponseContentDisposition = aws.String("inline")
	}
	req, err := s.presign.PresignGetObject(ctx, in, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3.PresignGet: %w", err)
	}
	return s.rewritePresignURL(req.URL), nil
}

// contentTypeFromKey infers a Content-Type from the file extension. Falls back
// to Go's mime registry for anything we don't explicitly cover. Returns the
// empty string when no type can be determined, in which case the caller
// should leave the response Content-Type alone.
func contentTypeFromKey(key string) string {
	ext := strings.ToLower(path.Ext(key))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".avif", ".svg":
		return mime.TypeByExtension(ext)
	case ".mp3":
		return "audio/mpeg"
	case ".wav":
		return "audio/wav"
	case ".ogg", ".opus":
		return "audio/ogg"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".flac":
		return "audio/flac"
	}
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return ""
}

// PresignPut returns a presigned URL for PUT. Clients must send
// x-amz-checksum-sha256; S3 validates before committing the object.
func (s *Store) PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error) {
	req, err := s.presign.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:            aws.String(s.bucket),
		Key:               aws.String(key),
		ContentType:       aws.String(contentType),
		ChecksumAlgorithm: types.ChecksumAlgorithmSha256,
	}, s3.WithPresignExpires(ttl))
	if err != nil {
		return "", fmt.Errorf("s3.PresignPut: %w", err)
	}
	return s.rewritePresignURL(req.URL), nil
}

// GetObjectAttributes returns the authoritative attributes upload completion
// needs. GetObjectAttributes supplies checksum fields, while HeadObject supplies
// HTTP metadata such as Content-Type.
func (s *Store) GetObjectAttributes(ctx context.Context, key string) (storage.ObjectAttrs, error) {
	out, err := s.client.GetObjectAttributes(ctx, &s3.GetObjectAttributesInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
		ObjectAttributes: []types.ObjectAttributes{
			types.ObjectAttributesChecksum,
			types.ObjectAttributesObjectSize,
			types.ObjectAttributesEtag,
		},
	})
	if err != nil {
		return storage.ObjectAttrs{}, fmt.Errorf("s3.GetObjectAttributes: %w", err)
	}
	head, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return storage.ObjectAttrs{}, fmt.Errorf("s3.HeadObject: %w", err)
	}
	attrs := storage.ObjectAttrs{}
	if out.ObjectSize != nil {
		attrs.SizeBytes = *out.ObjectSize
	}
	if out.ETag != nil {
		attrs.ETag = strings.Trim(*out.ETag, `"`)
	}
	if out.Checksum != nil && out.Checksum.ChecksumSHA256 != nil {
		attrs.SHA256Hex = checksumBase64ToHex(*out.Checksum.ChecksumSHA256)
	}
	if out.VersionId != nil {
		attrs.VersionID = *out.VersionId
	}
	if head.ContentType != nil {
		attrs.ContentType = *head.ContentType
	}
	return attrs, nil
}

// EnsureBucket creates the bucket if absent. Local/test only; production owns
// buckets via Terraform.
func (s *Store) EnsureBucket(ctx context.Context) error {
	_, err := s.client.CreateBucket(ctx, &s3.CreateBucketInput{Bucket: aws.String(s.bucket)})
	if err == nil {
		return nil
	}
	var owned *types.BucketAlreadyOwnedByYou
	var exists *types.BucketAlreadyExists
	if errors.As(err, &owned) || errors.As(err, &exists) {
		return nil
	}
	return err
}

func materialize(r io.Reader, providedSum string, providedSize int64) (io.Reader, string, int64, error) {
	if r == nil {
		return bytes.NewReader(nil), sha256OfEmpty, 0, nil
	}
	if seeker, ok := r.(io.ReadSeeker); ok {
		if providedSum != "" && providedSize > 0 {
			return seeker, providedSum, providedSize, nil
		}
		// Hash by streaming through the reader, then seek back so PutObject
		// reads the same bytes. Avoids buffering the body a second time when
		// the caller already passed a random-access source (e.g. bytes.Reader).
		h := sha256.New()
		n, err := io.Copy(h, seeker)
		if err != nil {
			return nil, "", 0, fmt.Errorf("s3: hash body: %w", err)
		}
		if _, err := seeker.Seek(0, io.SeekStart); err != nil {
			return nil, "", 0, fmt.Errorf("s3: rewind body: %w", err)
		}
		return seeker, hex.EncodeToString(h.Sum(nil)), n, nil
	}
	var buf bytes.Buffer
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(&buf, h), r)
	if err != nil {
		return nil, "", 0, fmt.Errorf("s3: read body: %w", err)
	}
	return bytes.NewReader(buf.Bytes()), hex.EncodeToString(h.Sum(nil)), n, nil
}

// sha256OfEmpty is hex(SHA-256("")) — the AWS expectation for an empty body.
var sha256OfEmpty = func() string {
	sum := sha256.Sum256(nil)
	return hex.EncodeToString(sum[:])
}()

func encodeTags(tags map[string]string) string {
	vals := make(url.Values, len(tags))
	for k, v := range tags {
		vals.Set(k, v)
	}
	return vals.Encode()
}

func checksumBase64ToHex(b64 string) string {
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return ""
	}
	return hex.EncodeToString(raw)
}
