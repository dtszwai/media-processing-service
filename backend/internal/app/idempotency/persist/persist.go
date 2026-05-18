// Package persist defines the persisted row shape for an idempotency claim.
// The shape is shared between two writer styles:
//
//   - Bracketed side-effects (provider call, webhook delivery): Claim → run →
//     Complete/Fail. The full Store protocol in app/idempotency drives this.
//   - Transactional submits where the result IS the transaction (e.g.
//     atomically stake a claim + insert result rows in one TransactWrite):
//     the row is pre-COMPLETED with a zero lease.
//
// Centralizing the layout keeps both writer styles on the same row format. The
// package intentionally exposes only layout primitives + a hash-aware reader,
// not the richer Claim/Complete/Fail port.
package persist

import (
	"context"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

// ClaimSK is the fixed sort key on every claim row.
const ClaimSK = "CLAIM"

// PK partitions a claim row by scope.
func PK(scope string) string { return "IDEMPOTENCY#" + scope }

// Key returns the kv.Key for a claim row.
func Key(scope string) kv.Key { return kv.Key{PK: PK(scope), SK: ClaimSK} }

// reservedClaimFields are the columns NewCompletedClaim always writes. They
// are stripped on metadata round-trip so a caller-supplied key can never
// displace the claim's own bookkeeping, and a future named field cannot be
// silently confused with caller metadata.
var reservedClaimFields = map[string]struct{}{
	"PK":          {},
	"SK":          {},
	"input_hash":  {},
	"status":      {},
	"result":      {},
	"claim_token": {},
	"lease_until": {},
	"attempts":    {},
	"ttl_epoch":   {},
	"created_at":  {},
	"updated_at":  {},
}

// CompletedClaimOption configures the optional fields of a pre-completed
// idempotency claim row. Use WithMetadata to stamp typed top-level fields
// that a replay path can read back via GetResultWithHashAndMetadata.
type CompletedClaimOption func(map[string]any)

// WithMetadata attaches typed top-level fields to the claim row. Keys must
// be stable, lowercase strings agreed between writer and reader (e.g.
// "tenant_id", "media_id", "asset_id" for upload-init replay). Reserved
// claim columns are dropped silently.
func WithMetadata(metadata map[string]string) CompletedClaimOption {
	return func(row map[string]any) {
		for k, v := range metadata {
			if _, reserved := reservedClaimFields[k]; reserved {
				continue
			}
			row[k] = v
		}
	}
}

// NewCompletedClaim builds a pre-completed idempotency claim row. Used by
// transactional submit paths that need to bake the claim into the same
// TransactWrite as the result rows (media presigned-upload init, derive
// enqueue, generation job submit). The row is already COMPLETED: there is no
// in-flight provider call to bracket — the side effect IS the transaction.
// Pass WithMetadata to stamp typed fields that a replay path can read
// without re-parsing the scope or result string.
func NewCompletedClaim(scope, inputHash, result string, now time.Time, ttl time.Duration, opts ...CompletedClaimOption) map[string]any {
	row := map[string]any{
		"PK":          PK(scope),
		"SK":          ClaimSK,
		"input_hash":  inputHash,
		"status":      string(idempotency.StatusCompleted),
		"result":      result,
		"claim_token": randid.New(),
		"lease_until": 0,
		"attempts":    1,
		"ttl_epoch":   now.Add(ttl).Unix(),
		"created_at":  now.Format(time.RFC3339Nano),
		"updated_at":  now.Format(time.RFC3339Nano),
	}
	for _, opt := range opts {
		opt(row)
	}
	return row
}

// GetResultWithHash reads a claim row's result + persisted input hash + status.
// Callers that replay a submit operation use the hash to detect the
// idempotency_key-with-different-input case (same scope, different hash):
// returning the cached result there would silently change what the caller
// sees, so this is treated as a usage error at the edge.
//
// Returns the raw idempotency.Status enum and lets the caller decide how
// CLAIMED/COMPLETED/FAILED map onto its surface (e.g. CLAIMED maps to Aborted
// on submit, COMPLETED maps to cached-replay everywhere else).
func GetResultWithHash(ctx context.Context, k kv.KV, scope string) (ref, inputHash string, statusOut idempotency.Status, err error) {
	var row struct {
		Result    string `dynamodbav:"result"`
		InputHash string `dynamodbav:"input_hash"`
		Status    string `dynamodbav:"status"`
	}
	if gerr := k.Get(ctx, Key(scope), &row); gerr != nil {
		return "", "", "", gerr
	}
	return row.Result, row.InputHash, idempotency.Status(row.Status), nil
}

// GetResultWithHashAndMetadata is GetResultWithHash plus the string-typed
// metadata fields the writer attached via WithMetadata. Reserved claim
// columns are stripped from the returned map so a caller sees only the
// typed fields it (or a previous writer) stamped. Returns kv.ErrNotFound
// when no row exists.
func GetResultWithHashAndMetadata(ctx context.Context, k kv.KV, scope string) (ref, inputHash string, statusOut idempotency.Status, metadata map[string]string, err error) {
	var row map[string]any
	if gerr := k.Get(ctx, Key(scope), &row); gerr != nil {
		return "", "", "", nil, gerr
	}
	ref, _ = row["result"].(string)
	inputHash, _ = row["input_hash"].(string)
	statusStr, _ := row["status"].(string)
	statusOut = idempotency.Status(statusStr)
	metadata = make(map[string]string)
	for k, v := range row {
		if _, reserved := reservedClaimFields[k]; reserved {
			continue
		}
		if s, ok := v.(string); ok {
			metadata[k] = s
		}
	}
	return ref, inputHash, statusOut, metadata, nil
}
