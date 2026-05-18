package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/util/shardkey"
)

// deriveOpSpec is the per-op record CreateAssets needs: the canonical
// AssetOperation enum the worker stamps on the row, plus the role string
// DeriveAssetID hashes into the stable id.
type deriveOpSpec struct {
	Operation  media.AssetOperation
	Role       string
	MediaTypes []media.Type
}

// allowedDeriveOps is the set of operations CreateAssets accepts. Stays in
// lockstep with what the derive worker (internal/app/derive) can produce —
// accepting an op the worker silently drops would be a contract bug.
var allowedDeriveOps = map[string]deriveOpSpec{
	"thumbnail": {Operation: media.AssetOperationImageThumbnail, Role: "thumbnail", MediaTypes: []media.Type{media.TypeImage}},
}

const createAssetsClaimTTL = 30 * 24 * time.Hour

// ErrIdempotencyKeyReused is returned when a CreateAssets call shares the
// caller-supplied idempotency_key with a previous call but the canonical
// hash of the inputs is different. The previous call's response is the
// source of truth and cannot be overwritten.
var ErrIdempotencyKeyReused = errors.New("media.CreateAssets: idempotency key reused with different input")

// ErrInvalidOperation is returned when the request contains an op that is
// not in AllowedDeriveOperations.
var ErrInvalidOperation = errors.New("media.CreateAssets: unknown operation")

// CreateAssetsInput is the application-layer request shape.
type CreateAssetsInput struct {
	TenantID       string
	MediaID        string
	IdempotencyKey string
	// Operations is the list of derive operations the caller wants. Must be
	// non-empty, deduplicated, and a subset of AllowedDeriveOperations.
	Operations []string
}

// AssetRef is a slim {operation, asset_id, lifecycle} tuple returned by the transport alongside an accepted derive request.
type AssetRef struct {
	Operation string `json:"operation"`
	AssetID   string `json:"asset_id"`
	Lifecycle string `json:"lifecycle"`
}

// CreateAssetsOutput is the application-layer response shape.
type CreateAssetsOutput struct {
	MediaID string     `json:"media_id"`
	Assets  []AssetRef `json:"assets"`
	// Replay reports whether this call hit the idempotency cache (true) or
	// staged a new outbox row (false). Useful for observability.
	Replay bool `json:"replay"`
}

// DeriveEnqueueInput is the cross-table derive-enqueue payload: stake the
// idempotency claim and stage the media.v1.process outbox row in one
// transaction. On collision the implementation reads back the previous claim
// and either replays (same input hash) or returns ErrIdempotencyKeyReused.
type DeriveEnqueueInput struct {
	TenantID  string
	MediaID   string
	Scope     string
	InputHash string
	Result    string
	ClaimTTL  time.Duration
	Row       OutboxRow
	Now       time.Time
}

// DeriveRepository is the storage-side capability CreateAssets needs. Distinct
// from Repository so callers can wire a different impl, and so a missing
// dependency fails at startup rather than at request time as a runtime type
// assertion. Implemented by the DDB repo; tests provide an in-memory fake.
type DeriveRepository interface {
	EnqueueDerive(ctx context.Context, in DeriveEnqueueInput) (cachedResult string, replay bool, err error)
}

// CreateAssets validates the request, computes deterministic asset ids,
// stakes the idempotency claim, and enqueues the derive event.
func (s *Service) CreateAssets(ctx context.Context, in CreateAssetsInput) (*CreateAssetsOutput, error) {
	if in.TenantID == "" || in.MediaID == "" {
		return nil, fmt.Errorf("%w: tenant_id + media_id required", ErrInvalidInput)
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" {
		return nil, fmt.Errorf("%w: idempotency_key required", ErrInvalidInput)
	}
	ops, err := normalizeOperations(in.Operations)
	if err != nil {
		return nil, err
	}

	m, err := s.Repo.GetMedia(ctx, in.TenantID, in.MediaID)
	if err != nil {
		return nil, err
	}
	if m.Lifecycle == media.LifecycleDeleted {
		return nil, fmt.Errorf("%w: media is soft-deleted", ErrPreconditionFailed)
	}
	if m.Lifecycle == media.LifecyclePending {
		return nil, fmt.Errorf("%w: media upload not yet completed", ErrPreconditionFailed)
	}
	if err := validateOperationsForMediaType(m.Type, ops); err != nil {
		return nil, err
	}

	if s.Derive == nil {
		return nil, errors.New("media.CreateAssets: derive repository not wired")
	}

	now := s.Now()
	inputHash := hashCreateAssetsInput(in.TenantID, in.MediaID, in.IdempotencyKey, ops)
	scope := "asset:" + in.TenantID + ":" + in.MediaID + ":" + in.IdempotencyKey
	// messageID is the outbox row's event_id and the input to deriveAssetID.
	// Deriving it from the input hash makes asset ids stable across replays
	// and across multiple stage attempts of the same logical request.
	messageID := "evt-derive-" + inputHash[:16]

	assetIDs := make(map[string]string, len(ops))
	for _, op := range ops {
		assetIDs[op] = DeriveAssetID(messageID, in.MediaID, allowedDeriveOps[op].Role)
	}

	resultJSON, err := json.Marshal(assetIDs)
	if err != nil {
		return nil, fmt.Errorf("media.CreateAssets: marshal result: %w", err)
	}

	evt := events.MediaEvent{
		MessageID:   messageID,
		EventType:   events.EventMediaProcess,
		TenantID:    in.TenantID,
		MediaID:     in.MediaID,
		Traceparent: extractTraceparent(ctx),
		CreatedAt:   now,
	}
	body, err := json.Marshal(evt)
	if err != nil {
		return nil, fmt.Errorf("media.CreateAssets: marshal event: %w", err)
	}
	row := OutboxRow{
		Stream:      outbox.StreamMedia,
		PartitionTS: now,
		Shard:       shardkey.Of(in.MediaID, 8),
		EventID:     messageID,
		Body:        body,
		EventType:   string(events.EventMediaProcess),
		TenantID:    in.TenantID,
	}

	cached, replay, eerr := s.Derive.EnqueueDerive(ctx, DeriveEnqueueInput{
		TenantID:  in.TenantID,
		MediaID:   in.MediaID,
		Scope:     scope,
		InputHash: inputHash,
		Result:    string(resultJSON),
		ClaimTTL:  createAssetsClaimTTL,
		Row:       row,
		Now:       now,
	})
	if eerr != nil {
		return nil, eerr
	}
	if replay {
		// Trust the cached id map over the recomputed one — if the input
		// hash changed the repo returns ErrIdempotencyKeyReused above.
		var cachedIDs map[string]string
		if uerr := json.Unmarshal([]byte(cached), &cachedIDs); uerr != nil {
			return nil, fmt.Errorf("media.CreateAssets: parse cached claim: %w", uerr)
		}
		assetIDs = cachedIDs
	}

	out := &CreateAssetsOutput{
		MediaID: in.MediaID,
		Assets:  make([]AssetRef, 0, len(ops)),
		Replay:  replay,
	}
	for _, op := range ops {
		out.Assets = append(out.Assets, AssetRef{
			Operation: op,
			AssetID:   assetIDs[op],
			Lifecycle: string(media.AssetLifecycleProcessing),
		})
	}
	return out, nil
}

// normalizeOperations validates each op, sorts the slice for stable hashing,
// and refuses duplicates. The sort gives us a canonical hash regardless of
// caller-supplied order.
func normalizeOperations(ops []string) ([]string, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("%w: operations required", ErrInvalidInput)
	}
	seen := make(map[string]struct{}, len(ops))
	out := make([]string, 0, len(ops))
	for _, raw := range ops {
		op := strings.ToLower(strings.TrimSpace(raw))
		if _, ok := allowedDeriveOps[op]; !ok {
			return nil, fmt.Errorf("%w: %q (allowed: %s)", ErrInvalidOperation, op, allowedOpsList())
		}
		if _, dup := seen[op]; dup {
			return nil, fmt.Errorf("%w: duplicate operation %q", ErrInvalidInput, op)
		}
		seen[op] = struct{}{}
		out = append(out, op)
	}
	sort.Strings(out)
	return out, nil
}

func validateOperationsForMediaType(mediaType media.Type, ops []string) error {
	if !supportsAnyDeriveOperation(mediaType) {
		return fmt.Errorf("%w: derive not supported for media type %q", ErrPreconditionFailed, mediaType)
	}
	for _, op := range ops {
		if !supportsMediaType(allowedDeriveOps[op], mediaType) {
			return fmt.Errorf("%w: operation %q not supported for media type %q", ErrPreconditionFailed, op, mediaType)
		}
	}
	return nil
}

func supportsAnyDeriveOperation(mediaType media.Type) bool {
	for _, spec := range allowedDeriveOps {
		if supportsMediaType(spec, mediaType) {
			return true
		}
	}
	return false
}

func supportsMediaType(spec deriveOpSpec, mediaType media.Type) bool {
	for _, t := range spec.MediaTypes {
		if t == mediaType {
			return true
		}
	}
	return false
}

func allowedOpsList() string {
	keys := make([]string, 0, len(allowedDeriveOps))
	for k := range allowedDeriveOps {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

// hashCreateAssetsInput produces the canonical hash for the idempotency
// claim. Operations are pre-sorted by normalizeOperations so the hash is
// order-independent for the caller.
func hashCreateAssetsInput(tenantID, mediaID, key string, ops []string) string {
	parts := make([]string, 0, 3+len(ops))
	parts = append(parts, tenantID, mediaID, key)
	parts = append(parts, ops...)
	return idempotency.HashInputs(parts...)
}

// DeriveAssetID is the canonical mapping (messageID, mediaID, role) → stable
// asset id used by both the derive worker and CreateAssets so the ids the
// API returns are exactly the rows the worker writes.
func DeriveAssetID(messageID, mediaID, role string) string {
	sum := sha256.Sum256([]byte(messageID + "|" + mediaID + "|" + role))
	return "ast_" + hex.EncodeToString(sum[:8])
}
