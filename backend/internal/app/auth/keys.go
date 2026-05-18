package auth

import (
	"errors"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// Sentinel errors. Re-exported by the DDB impl on the corresponding outcomes.
var (
	ErrEmailTaken = errors.New("auth: email already registered")
	ErrNotFound   = kv.ErrNotFound
)

// UserPK returns the partition key for a user profile row.
func UserPK(userID string) string { return "USER#" + userID }

// UserProfileSK is the fixed SK on profile rows.
const UserProfileSK = "PROFILE"

// EmailLookupPK partitions the email→user lookup row. Email is lower-cased so
// logins are case-insensitive while the canonical email stays on the profile.
func EmailLookupPK(email string) string {
	return "EMAIL#" + strings.ToLower(email)
}

// EmailLookupSK is the fixed SK on lookup rows.
const EmailLookupSK = "LOOKUP"

// APIKeyHashPK partitions a key-hash auth row. Key is sha256-hashed so the raw
// value never leaves the client.
func APIKeyHashPK(keyHash string) string { return "APIKEY#" + keyHash }

// APIKeyHashSK is the fixed SK on auth rows.
const APIKeyHashSK = "META"

// TenantAPIKeysPK partitions the per-tenant API-key index for list/delete.
func TenantAPIKeysPK(tenantID string) string { return "TENANT#" + tenantID + "#APIKEY" }

// TenantAPIKeySK returns the per-key SK on tenant index rows.
func TenantAPIKeySK(keyID string) string { return "KEY#" + keyID }
