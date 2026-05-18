package ddb

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/storage"
)

func TestStaging_LoadStagedRejectsExpiredTrackingRow(t *testing.T) {
	t0 := time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC)
	blob := &stagingBlob{}
	db := &stagingKV{rows: map[string]map[string]any{
		StagedPK("tenant-1", "job-1") + "\x00" + stagedSK: {
			"content_type": "application/octet-stream",
			"extension":    "bin",
			"sha256":       "sha",
			"metadata":     map[string]string{"provider": "test"},
			"expires_at":   t0.Add(time.Minute).Format(time.RFC3339Nano),
			"ttl_epoch":    t0.Add(time.Minute).Unix(),
		},
	}}
	stager := NewStaging(db, blob)
	stager.Now = func() time.Time { return t0.Add(2 * time.Minute) }

	_, err := stager.LoadStaged(context.Background(), genapp.StagedRef{
		StorageKey: "provider-staging/generation/tenant-1/media/job-1.bin",
		TenantID:   "tenant-1",
		JobID:      "job-1",
	})
	if !errors.Is(err, genapp.ErrStagedNotFound) {
		t.Fatalf("LoadStaged expired err = %v, want ErrStagedNotFound", err)
	}
	if blob.gets != 0 {
		t.Fatalf("blob Get calls = %d, want 0 for expired tracking row", blob.gets)
	}
}

type stagingKV struct {
	rows map[string]map[string]any
}

func (f *stagingKV) Get(_ context.Context, key kv.Key, out any) error {
	row, ok := f.rows[key.PK+"\x00"+key.SK]
	if !ok {
		return kv.ErrNotFound
	}
	dst, ok := out.(*stagedRow)
	if !ok {
		return errors.New("stagingKV: unsupported Get target")
	}
	dst.ContentType, _ = row["content_type"].(string)
	dst.Extension, _ = row["extension"].(string)
	dst.SHA256, _ = row["sha256"].(string)
	dst.Metadata, _ = row["metadata"].(map[string]string)
	dst.ExpiresAt, _ = row["expires_at"].(string)
	dst.TTLEpoch, _ = row["ttl_epoch"].(int64)
	return nil
}

func (f *stagingKV) Put(context.Context, kv.Item, kv.PutOptions) error {
	return errors.New("stagingKV: Put not supported")
}

func (f *stagingKV) Query(context.Context, kv.QueryRequest) (kv.QueryResult, error) {
	return kv.QueryResult{}, errors.New("stagingKV: Query not supported")
}

func (f *stagingKV) Update(context.Context, kv.UpdateOp) error {
	return errors.New("stagingKV: Update not supported")
}

func (f *stagingKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, errors.New("stagingKV: UpdateReturning not supported")
}

func (f *stagingKV) Delete(context.Context, kv.DeleteOp) error {
	return errors.New("stagingKV: Delete not supported")
}

func (f *stagingKV) TransactWrite(context.Context, []kv.WriteOp) error {
	return errors.New("stagingKV: TransactWrite not supported")
}

type stagingBlob struct {
	gets int
}

func (b *stagingBlob) Put(context.Context, storage.PutInput) (storage.PutOutput, error) {
	return storage.PutOutput{}, errors.New("stagingBlob: Put not supported")
}

func (b *stagingBlob) Get(context.Context, string) (io.ReadCloser, error) {
	b.gets++
	return io.NopCloser(strings.NewReader("staged")), nil
}

func (b *stagingBlob) Delete(context.Context, string) error { return nil }

func (b *stagingBlob) PresignGet(context.Context, string, time.Duration) (string, error) {
	return "", errors.New("stagingBlob: PresignGet not supported")
}

func (b *stagingBlob) PresignPut(context.Context, string, string, time.Duration) (string, error) {
	return "", errors.New("stagingBlob: PresignPut not supported")
}

func (b *stagingBlob) GetObjectAttributes(context.Context, string) (storage.ObjectAttrs, error) {
	return storage.ObjectAttrs{}, errors.New("stagingBlob: GetObjectAttributes not supported")
}

func (b *stagingBlob) HeadMetadata(context.Context, string) (map[string]string, error) {
	return nil, errors.New("stagingBlob: HeadMetadata not supported")
}

var _ kv.KV = (*stagingKV)(nil)
var _ storage.Storage = (*stagingBlob)(nil)
