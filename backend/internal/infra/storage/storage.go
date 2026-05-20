// Package storage is the object-store port. Drivers (storage/impl/*) implement it.
package storage

import (
	"context"
	"io"
	"time"
)

// PutInput is the write payload.
type PutInput struct {
	Key         string
	Body        io.Reader
	ContentType string
	SizeBytes   int64
	SHA256Hex   string // optional; computed by driver when empty
	Tags        map[string]string
	Metadata    map[string]string
}

// PutOutput is what the driver actually persisted.
type PutOutput struct {
	Key       string
	SHA256Hex string
	SizeBytes int64
	ETag      string
}

// ObjectAttrs is the subset of metadata callers need at upload-complete time.
//
// VersionID carries the per-PUT S3 object version. Together with the bucket's
// `aws_s3_bucket_versioning` Enabled config it is the stable discriminator
// the upload-completion idempotency scope keys on, so two completion attempts
// on the same uploaded bytes collapse to one row mutation.
type ObjectAttrs struct {
	SizeBytes   int64
	ContentType string
	ETag        string
	SHA256Hex   string
	VersionID   string
}

// Storage is the data-plane port: object Put/Get/Delete plus presign + attrs.
type Storage interface {
	Put(ctx context.Context, in PutInput) (PutOutput, error)
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	PresignGet(ctx context.Context, key string, ttl time.Duration) (string, error)
	PresignPut(ctx context.Context, key, contentType string, ttl time.Duration) (string, error)
	GetObjectAttributes(ctx context.Context, key string) (ObjectAttrs, error)
}
