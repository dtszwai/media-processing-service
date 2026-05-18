package webhook

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

type SecretSealer interface {
	Seal(ctx context.Context, tenantID, jobID, plaintext string) ([]byte, error)
	Unseal(ctx context.Context, tenantID, jobID string, ciphertext []byte) (string, error)
}

type SecretResolver interface {
	ActiveSecret(ctx context.Context, tenantID string) (keyID string, secret []byte, err error)
	ResolveSecret(ctx context.Context, tenantID, keyID string) ([]byte, error)
}

type SecretStore struct {
	KV     kv.KV
	Sealer SecretSealer
	Now    func() time.Time
}

func NewSecretStore(k kv.KV, sealer SecretSealer) *SecretStore {
	return &SecretStore{KV: k, Sealer: sealer, Now: func() time.Time { return time.Now().UTC() }}
}

func (s *SecretStore) Rotate(ctx context.Context, tenantID string) (oldKeyID, newKeyID string, err error) {
	if tenantID == "" {
		return "", "", errors.New("webhook secrets: tenant_id required")
	}
	if s == nil || s.KV == nil || s.Sealer == nil {
		return "", "", errors.New("webhook secrets: kv and sealer required")
	}
	oldKeyID, _ = s.activeKeyID(ctx, tenantID)
	newKeyID = "whsec_" + randid.New()
	raw, err := randomSecret()
	if err != nil {
		return "", "", err
	}
	encrypted, err := s.Sealer.Seal(ctx, tenantID, secretContext(newKeyID), raw)
	if err != nil {
		return "", "", err
	}
	now := s.Now().UTC()
	err = s.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{
			Item: map[string]any{
				"PK":               secretPK(tenantID),
				"SK":               secretKeySK(newKeyID),
				"item_type":        "WEBHOOK_SECRET",
				"tenant_id":        tenantID,
				"key_id":           newKeyID,
				"encrypted_secret": encrypted,
				"created_at":       now.Format(time.RFC3339Nano),
				"ttl_epoch":        now.Add(365 * 24 * time.Hour).Unix(),
			},
			ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
		}},
		{Update: &kv.UpdateOp{
			Key:              kv.Key{PK: secretPK(tenantID), SK: "ACTIVE"},
			UpdateExpression: "SET item_type = :item_type, tenant_id = :tenant_id, active_key_id = :new, previous_key_id = :old, updated_at = :now, created_at = if_not_exists(created_at, :now)",
			ExpressionAttributeValues: kv.Values{
				":item_type": "WEBHOOK_SECRET_ACTIVE",
				":tenant_id": tenantID,
				":new":       newKeyID,
				":old":       oldKeyID,
				":now":       now.Format(time.RFC3339Nano),
			},
		}},
	})
	if err != nil {
		return "", "", err
	}
	return oldKeyID, newKeyID, nil
}

func (s *SecretStore) ActiveSecret(ctx context.Context, tenantID string) (string, []byte, error) {
	keyID, err := s.activeKeyID(ctx, tenantID)
	if err != nil {
		return "", nil, err
	}
	secret, err := s.ResolveSecret(ctx, tenantID, keyID)
	return keyID, secret, err
}

func (s *SecretStore) ResolveSecret(ctx context.Context, tenantID, keyID string) ([]byte, error) {
	if tenantID == "" || keyID == "" {
		return nil, errors.New("webhook secrets: tenant_id and key_id required")
	}
	var row struct {
		EncryptedSecret []byte `dynamodbav:"encrypted_secret"`
	}
	if err := s.KV.Get(ctx, kv.Key{PK: secretPK(tenantID), SK: secretKeySK(keyID)}, &row); err != nil {
		return nil, err
	}
	plain, err := s.Sealer.Unseal(ctx, tenantID, secretContext(keyID), row.EncryptedSecret)
	if err != nil {
		return nil, err
	}
	return []byte(plain), nil
}

func (s *SecretStore) activeKeyID(ctx context.Context, tenantID string) (string, error) {
	var row struct {
		ActiveKeyID string `dynamodbav:"active_key_id"`
	}
	if err := s.KV.Get(ctx, kv.Key{PK: secretPK(tenantID), SK: "ACTIVE"}, &row); err != nil {
		return "", err
	}
	if row.ActiveKeyID == "" {
		return "", kv.ErrNotFound
	}
	return row.ActiveKeyID, nil
}

func randomSecret() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func secretPK(tenantID string) string { return "WEBHOOK_SECRET#" + tenantID }

func secretKeySK(keyID string) string { return "KEY#" + keyID }

func secretContext(keyID string) string { return "webhook:" + keyID }
