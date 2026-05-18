// Package sealer is the at-rest envelope-encryption port.
//
// The positional (tenantID, jobID) baggage is locked: KMS drivers bind them as
// EncryptionContext so changing the signature requires a re-encrypt migration.
package sealer

import "context"

// Sealer seals/unseals byte blobs bound to a (tenant, job) context.
type Sealer interface {
	Seal(ctx context.Context, tenantID, jobID, plaintext string) ([]byte, error)
	Unseal(ctx context.Context, tenantID, jobID string, ciphertext []byte) (string, error)
}
