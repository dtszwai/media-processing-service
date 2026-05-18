// Package genprovider defines the contract every generation backend
// satisfies and ships the adapter family that implements it. Adapters live
// in sibling subpackages (codex, simulated, notebooklm). Vendor-specific
// configuration is captured at constructor time; the runtime interface stays
// minimal so workflow code never names a concrete vendor.
package genprovider

import (
	"context"
	"errors"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// ErrNotSupported is returned by adapter methods that do not apply to their
// call path: sync-only adapters return it from the async methods, and async
// adapters return it from GenerateSync.
var ErrNotSupported = errors.New("genprovider: operation not supported on this call path")

// Artifact.Metadata keys the AI-disclosure gate (app/generation.gate.go)
// inspects before promoting a provider artifact to a customer-visible asset.
// Producers and the consumer must agree on the exact strings — keeping them
// here is the single source of truth.
const (
	MetaProviderKey         = "provider"
	MetaModelKey            = "model"
	MetaIsAIGeneratedKey    = "is_ai_generated"
	MetaDisclosureKey       = "disclosure"
	MetaVisibleWatermarkKey = "visible_watermark"
	MetaContentSafetyKey    = "content_safety"
)

// DisclosureAIGenerated is the canonical disclosure value. Provider adapters
// MUST set MetaDisclosureKey to this on every artifact; the gate rejects
// anything else.
const DisclosureAIGenerated = "AI_GENERATED_DISCLOSURE"

// Provider is the unified generation adapter surface. InlineBytes() selects
// which call path is used:
//
//   - InlineBytes=true  → call GenerateSync; artifact bytes are returned
//     inline. Async methods MUST return ErrNotSupported.
//   - InlineBytes=false → call SubmitAsync (idempotent via ClientRequestID),
//     then PollAsync repeatedly until it returns PollReady or PollFailed,
//     then FetchAsync once. GenerateSync MUST return ErrNotSupported.
//
// Idempotency contract: SubmitAsync receives spec.ClientRequestID and must
// forward it as an Idempotency-Key (or vendor equivalent) so a crash-and-retry
// after SubmitAsync but before the persisted stage transition re-submits to
// the same provider job.
type Provider interface {
	InlineBytes() bool

	// Sync path (InlineBytes=true).
	GenerateSync(ctx context.Context, spec generation.JobSpec) (generation.Artifact, error)

	// Async path (InlineBytes=false).
	SubmitAsync(ctx context.Context, spec generation.JobSpec) (providerJobID string, err error)
	PollAsync(ctx context.Context, providerJobID string) (generation.PollStatus, error)
	FetchAsync(ctx context.Context, providerJobID string) (generation.Artifact, error)
}

// Named is the provider-identity port telemetry uses to tag metrics. The
// value must be the same low-cardinality vendor string the adapter writes
// to Artifact.Metadata[MetaProviderKey] so the observability label space
// matches the provenance recorded on the artifact.
type Named interface {
	Name() string
}

type VendorIdempotencyMode string

const (
	VendorIdempotencySupported   VendorIdempotencyMode = "SUPPORTED"
	VendorIdempotencyBestEffort  VendorIdempotencyMode = "BEST_EFFORT"
	VendorIdempotencyUnsupported VendorIdempotencyMode = "UNSUPPORTED"
)

type IdempotencyDeclarer interface {
	VendorIdempotency() VendorIdempotencyMode
}

func VendorIdempotency(p Provider) VendorIdempotencyMode {
	if d, ok := p.(IdempotencyDeclarer); ok {
		return d.VendorIdempotency()
	}
	return VendorIdempotencyBestEffort
}

// SyncOnly is an embeddable mixin that satisfies the async half of Provider
// with ErrNotSupported. Embed it in any adapter whose InlineBytes() returns
// true so the adapter does not have to restate the three async stubs.
type SyncOnly struct{}

func (SyncOnly) SubmitAsync(context.Context, generation.JobSpec) (string, error) {
	return "", ErrNotSupported
}

func (SyncOnly) PollAsync(context.Context, string) (generation.PollStatus, error) {
	return "", ErrNotSupported
}

func (SyncOnly) FetchAsync(context.Context, string) (generation.Artifact, error) {
	return generation.Artifact{}, ErrNotSupported
}
