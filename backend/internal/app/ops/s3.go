package ops

import (
	"context"
	"fmt"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Node is one node in the bucket tree. is_prefix=true marks a directory
// (key ending in /) rather than an object.
type S3Node struct {
	Key          string
	Name         string
	IsPrefix     bool
	SizeBytes    int64
	ETag         string
	LastModified time.Time
}

// ListS3 returns one directory level under prefix. The console renders the
// bucket as a `tenant/media/asset` tree; using delimiter="/" collapses
// listings into directory-style entries.
func (s *Service) ListS3(ctx context.Context, prefix, delimiter string, limit int32) ([]S3Node, error) {
	if s.Blob == nil {
		return nil, fmt.Errorf("ops: s3 client not wired")
	}
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	if delimiter == "" {
		delimiter = "/"
	}
	client, ok := s.s3Client()
	if !ok {
		return nil, fmt.Errorf("ops: s3 client unavailable")
	}
	in := &s3.ListObjectsV2Input{
		Bucket:    aws.String(s.Bucket),
		Prefix:    aws.String(prefix),
		Delimiter: aws.String(delimiter),
		MaxKeys:   aws.Int32(limit),
	}
	out, err := client.ListObjectsV2(ctx, in)
	if err != nil {
		return nil, fmt.Errorf("ops: list s3: %w", err)
	}
	nodes := make([]S3Node, 0, len(out.CommonPrefixes)+len(out.Contents))
	for _, p := range out.CommonPrefixes {
		key := aws.ToString(p.Prefix)
		nodes = append(nodes, S3Node{
			Key:      key,
			Name:     path.Base(strings.TrimSuffix(key, "/")),
			IsPrefix: true,
		})
	}
	for _, obj := range out.Contents {
		key := aws.ToString(obj.Key)
		node := S3Node{
			Key:  key,
			Name: path.Base(key),
		}
		if obj.Size != nil {
			node.SizeBytes = *obj.Size
		}
		if obj.ETag != nil {
			node.ETag = strings.Trim(*obj.ETag, `"`)
		}
		if obj.LastModified != nil {
			node.LastModified = *obj.LastModified
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].IsPrefix != nodes[j].IsPrefix {
			return nodes[i].IsPrefix
		}
		return nodes[i].Name < nodes[j].Name
	})
	return nodes, nil
}

// s3Client returns the s3 client off the storage driver via a narrow
// interface assertion. The Blob field is the public-port shape, but the
// underlying *s3.Store also implements raw operations the ops surface
// needs (ListObjects, DeleteObject for tree views).
func (s *Service) s3Client() (*s3.Client, bool) {
	type clientHolder interface {
		Client() *s3.Client
	}
	if h, ok := s.Blob.(clientHolder); ok {
		return h.Client(), true
	}
	return nil, false
}

// PresignDownload returns a 15-minute presigned GET URL.
func (s *Service) PresignDownload(ctx context.Context, key string) (string, time.Time, error) {
	if key == "" {
		return "", time.Time{}, fmt.Errorf("ops: key required")
	}
	const ttl = 15 * time.Minute
	url, err := s.Blob.PresignGet(ctx, key, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	return url, s.now().Add(ttl), nil
}

// DeleteS3Object hard-deletes an object. Audited. The console exposes this
// for ad-hoc cleanup; it is NOT the soft-delete path Media takes on the
// reaper.
func (s *Service) DeleteS3Object(ctx context.Context, key string) error {
	if key == "" {
		return fmt.Errorf("ops: key required")
	}
	if err := s.Blob.Delete(ctx, key); err != nil {
		return err
	}
	s.audit(ctx, AuditEvent{Operation: "DeleteS3Object", Target: key})
	return nil
}
