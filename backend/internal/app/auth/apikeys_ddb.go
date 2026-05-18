package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/user"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// DDBAPIKeys persists API keys as a pair of rows: a hash-lookup row used by
// the auth middleware, plus a per-tenant index row used for listing/deletion.
// The raw key value is never persisted.
type DDBAPIKeys struct {
	KV kv.KV
}

// NewDDBAPIKeys binds the impl to a kv driver.
func NewDDBAPIKeys(k kv.KV) *DDBAPIKeys { return &DDBAPIKeys{KV: k} }

// HashRawKey is the canonical hashing function for API keys.
func HashRawKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

type authRow struct {
	PK        string    `dynamodbav:"PK"`
	SK        string    `dynamodbav:"SK"`
	ItemType  string    `dynamodbav:"item_type"`
	KeyID     string    `dynamodbav:"key_id"`
	TenantID  string    `dynamodbav:"tenant_id"`
	UserID    string    `dynamodbav:"user_id"`
	Name      string    `dynamodbav:"name"`
	CreatedAt time.Time `dynamodbav:"created_at"`
}

type tenantRow struct {
	PK        string    `dynamodbav:"PK"`
	SK        string    `dynamodbav:"SK"`
	ItemType  string    `dynamodbav:"item_type"`
	KeyID     string    `dynamodbav:"key_id"`
	KeyHash   string    `dynamodbav:"key_hash"`
	TenantID  string    `dynamodbav:"tenant_id"`
	UserID    string    `dynamodbav:"user_id"`
	Name      string    `dynamodbav:"name"`
	CreatedAt time.Time `dynamodbav:"created_at"`
}

// Create writes auth + tenant index rows atomically.
func (r *DDBAPIKeys) Create(ctx context.Context, k user.APIKey, rawKey string) error {
	if k.ID == "" || k.TenantID == "" || k.UserID == "" || rawKey == "" {
		return errors.New("apikey: id, tenant_id, user_id, raw required")
	}
	if k.CreatedAt.IsZero() {
		k.CreatedAt = time.Now().UTC()
	}
	hash := HashRawKey(rawKey)
	auth := authRow{
		PK: APIKeyHashPK(hash), SK: APIKeyHashSK, ItemType: "APIKEY",
		KeyID: k.ID, TenantID: k.TenantID, UserID: k.UserID, Name: k.Name, CreatedAt: k.CreatedAt,
	}
	tenant := tenantRow{
		PK: TenantAPIKeysPK(k.TenantID), SK: TenantAPIKeySK(k.ID), ItemType: "APIKEY_INDEX",
		KeyID: k.ID, KeyHash: hash, TenantID: k.TenantID, UserID: k.UserID, Name: k.Name, CreatedAt: k.CreatedAt,
	}
	return r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                auth,
			ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
		}},
		{Put: &kv.PutOp{
			Item:                tenant,
			ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
		}},
	})
}

// ListByTenant returns metadata for every key under the tenant. The hash is
// intentionally not returned — list responses must never echo it.
func (r *DDBAPIKeys) ListByTenant(ctx context.Context, tenantID string) ([]user.APIKey, error) {
	var out []user.APIKey
	var start *kv.Key
	for {
		page, err := r.KV.Query(ctx, kv.QueryRequest{
			KeyConditionExpression: "PK = :pk AND begins_with(SK, :sk)",
			ExpressionAttributeValues: kv.Values{
				":pk": TenantAPIKeysPK(tenantID),
				":sk": "KEY#",
			},
			ExclusiveStartKey: start,
		})
		if err != nil {
			return nil, err
		}
		if out == nil {
			out = make([]user.APIKey, 0, len(page.Items))
		}
		for _, item := range page.Items {
			var row tenantRow
			if uerr := item.Unmarshal(&row); uerr != nil {
				return nil, uerr
			}
			out = append(out, user.APIKey{
				ID:        row.KeyID,
				TenantID:  row.TenantID,
				UserID:    row.UserID,
				Name:      row.Name,
				CreatedAt: row.CreatedAt,
			})
		}
		if page.LastEvaluatedKey == nil {
			return out, nil
		}
		start = page.LastEvaluatedKey
	}
}

// Delete looks up the tenant row to find the hash then deletes both rows
// atomically, scoped to tenantID so cross-tenant deletes fail.
func (r *DDBAPIKeys) Delete(ctx context.Context, tenantID, keyID string) error {
	var row tenantRow
	if err := r.KV.Get(ctx, kv.Key{PK: TenantAPIKeysPK(tenantID), SK: TenantAPIKeySK(keyID)}, &row); err != nil {
		return err
	}
	return r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Delete: &kv.DeleteOp{Key: kv.Key{PK: APIKeyHashPK(row.KeyHash), SK: APIKeyHashSK}}},
		{Delete: &kv.DeleteOp{Key: kv.Key{PK: TenantAPIKeysPK(tenantID), SK: TenantAPIKeySK(keyID)}}},
	})
}

// LookupByRaw resolves an inbound raw key to its metadata.
func (r *DDBAPIKeys) LookupByRaw(ctx context.Context, rawKey string) (*user.APIKey, error) {
	if rawKey == "" {
		return nil, errors.New("apikey: raw required")
	}
	var row authRow
	if err := r.KV.Get(ctx, kv.Key{PK: APIKeyHashPK(HashRawKey(rawKey)), SK: APIKeyHashSK}, &row); err != nil {
		return nil, err
	}
	// KeyHash deliberately not echoed back to callers.
	return &user.APIKey{
		ID:        row.KeyID,
		TenantID:  row.TenantID,
		UserID:    row.UserID,
		Name:      row.Name,
		CreatedAt: row.CreatedAt,
	}, nil
}
