// Package shorturl implements the short-URL allocator and resolver.
// DDB layout: PK/SK = SHORT#{code}/SHORT#{code} for primary rows;
// SHORT_INDEX#{tenant}#{media}/SHORT#{code} for the per-media index.
package shorturl

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// ErrNotFound is returned by Resolve / Revoke when no row matches the code.
var ErrNotFound = errors.New("shorturl: not found")

// Record represents a short URL row.
type Record struct {
	Code      string `dynamodbav:"code"`
	TenantID  string `dynamodbav:"tenant_id"`
	MediaID   string `dynamodbav:"media_id,omitempty"`
	AssetID   string `dynamodbav:"asset_id,omitempty"`
	TargetURL string `dynamodbav:"target_url,omitempty"`
	Label     string `dynamodbav:"label,omitempty"`
	CreatedBy string `dynamodbav:"created_by,omitempty"`
	CreatedAt string `dynamodbav:"created_at"`
	ExpiresAt int64  `dynamodbav:"expires_at,omitempty"`
	RevokedAt string `dynamodbav:"revoked_at,omitempty"`
}

// Service is the shorturl application service.
type Service struct {
	KV      kv.KV
	codeLen int
}

// NewService binds the service to a kv driver.
func NewService(k kv.KV) *Service {
	return &Service{KV: k, codeLen: 8}
}

// Allocate returns a freshly generated code mapped to (tenant, media, asset)
// or to a target URL. Retries up to 10 times on collisions.
func (s *Service) Allocate(ctx context.Context, in Record) (string, error) {
	if in.TenantID == "" {
		return "", errors.New("shorturl: tenant_id required")
	}
	if in.TargetURL == "" && (in.MediaID == "" || in.AssetID == "") {
		return "", errors.New("shorturl: target_url or (media_id, asset_id) required")
	}
	if in.CreatedAt == "" {
		in.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	for attempt := 0; attempt < 10; attempt++ {
		code, err := randomCode(s.codeLen)
		if err != nil {
			return "", err
		}
		in.Code = code
		err = s.put(ctx, in)
		if err == nil {
			return code, nil
		}
		if errors.Is(err, kv.ErrConditionFailed) {
			continue
		}
		return "", err
	}
	return "", errors.New("shorturl: exhausted code attempts")
}

func (s *Service) put(ctx context.Context, in Record) error {
	ops := []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                primaryItem(in),
			ConditionExpression: "attribute_not_exists(PK)",
		}},
	}
	if idx := indexItem(in); idx != nil {
		ops = append(ops, kv.WriteOp{Put: &kv.PutOp{Item: idx}})
	}
	return s.KV.TransactWrite(ctx, ops)
}

func primaryItem(in Record) map[string]any {
	pk := "SHORT#" + in.Code
	item := map[string]any{
		"PK":         pk,
		"SK":         pk,
		"code":       in.Code,
		"tenant_id":  in.TenantID,
		"created_at": in.CreatedAt,
	}
	if in.MediaID != "" {
		item["media_id"] = in.MediaID
	}
	if in.AssetID != "" {
		item["asset_id"] = in.AssetID
	}
	if in.TargetURL != "" {
		item["target_url"] = in.TargetURL
	}
	if in.Label != "" {
		item["label"] = in.Label
	}
	if in.CreatedBy != "" {
		item["created_by"] = in.CreatedBy
	}
	if in.ExpiresAt > 0 {
		item["expires_at"] = in.ExpiresAt
	}
	return item
}

func indexItem(in Record) map[string]any {
	if in.MediaID == "" || in.TenantID == "" {
		return nil
	}
	item := map[string]any{
		"PK":         "SHORT_INDEX#" + in.TenantID + "#" + in.MediaID,
		"SK":         "SHORT#" + in.Code,
		"code":       in.Code,
		"tenant_id":  in.TenantID,
		"media_id":   in.MediaID,
		"asset_id":   in.AssetID,
		"created_at": in.CreatedAt,
	}
	if in.Label != "" {
		item["label"] = in.Label
	}
	if in.CreatedBy != "" {
		item["created_by"] = in.CreatedBy
	}
	if in.ExpiresAt > 0 {
		item["expires_at"] = in.ExpiresAt
	}
	return item
}

// ListByMedia returns every short URL pointing at (tenant, media). Bounded.
func (s *Service) ListByMedia(ctx context.Context, tenantID, mediaID string, limit int32) ([]Record, error) {
	if tenantID == "" || mediaID == "" {
		return nil, errors.New("shorturl: tenant_id and media_id required")
	}
	if limit <= 0 {
		limit = 50
	}
	page, err := s.KV.Query(ctx, kv.QueryRequest{
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :sk)",
		ExpressionAttributeValues: kv.Values{
			":pk": "SHORT_INDEX#" + tenantID + "#" + mediaID,
			":sk": "SHORT#",
		},
		Limit: limit,
	})
	if err != nil {
		return nil, err
	}
	rows := make([]Record, 0, len(page.Items))
	for _, item := range page.Items {
		var rec Record
		if uerr := item.Unmarshal(&rec); uerr != nil {
			return nil, uerr
		}
		rows = append(rows, rec)
	}
	return rows, nil
}

// Revoke deletes both rows after asserting the caller's tenant owns the code.
func (s *Service) Revoke(ctx context.Context, tenantID, code string) error {
	if tenantID == "" || code == "" {
		return errors.New("shorturl: tenant_id and code required")
	}
	rec, err := s.Resolve(ctx, code)
	if err != nil {
		return err
	}
	if rec.TenantID != tenantID {
		return errors.New("shorturl: cross-tenant denied")
	}
	pk := "SHORT#" + code
	ops := []kv.WriteOp{
		{Delete: &kv.DeleteOp{Key: kv.Key{PK: pk, SK: pk}}},
	}
	if rec.MediaID != "" {
		ops = append(ops, kv.WriteOp{Delete: &kv.DeleteOp{
			Key: kv.Key{PK: "SHORT_INDEX#" + tenantID + "#" + rec.MediaID, SK: "SHORT#" + code},
		}})
	}
	return s.KV.TransactWrite(ctx, ops)
}

// Resolve returns the Record for the given code.
func (s *Service) Resolve(ctx context.Context, code string) (*Record, error) {
	pk := "SHORT#" + code
	var rec Record
	if err := s.KV.Get(ctx, kv.Key{PK: pk, SK: pk}, &rec); err != nil {
		if errors.Is(err, kv.ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &rec, nil
}

var shortCodeEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

func randomCode(n int) (string, error) {
	b := make([]byte, (n*5+7)/8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	out := shortCodeEncoding.EncodeToString(b)
	if len(out) > n {
		out = out[:n]
	}
	return out, nil
}
