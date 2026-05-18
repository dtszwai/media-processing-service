// Package safety holds the application-layer safety ports the generation FSM
// brackets provider work with. INPUT_MODERATION runs before any cost is
// reserved so prompts that should never reach a provider don't burn tenant
// budget; OUTPUT_MODERATION runs after provider work, before the disclosure
// pipeline mutates the artifact, so platform-attested safety verdicts cover
// the bytes a customer would otherwise see.
//
// The port is intentionally narrow — a single Moderate call that returns a
// safety.Verdict. The full safety case (per-category scores, evidence hashes)
// lives in domain/safety and is composed by the workflow into one Verdict
// per (layer, subject) so audit rows stay a single shape regardless of which
// provider produced them.
package safety

import (
	"context"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
)

// ModerateInput is the request payload the workflow hands a Moderator.
// Exactly one of Prompt or Artifact carries the subject under review:
// INPUT_MODERATION sets Prompt; OUTPUT_MODERATION sets Artifact. Splitting
// the shape across two fields rather than typing it polymorphically keeps
// the local-classifier impl trivial and matches the on-the-wire payload
// real moderation providers expose (text endpoint vs image/audio endpoint).
type ModerateInput struct {
	Layer      safety.Layer
	TenantID   string
	JobID      string
	OutputType generation.OutputType
	Prompt     string
	Artifact   *generation.Artifact
}

// Moderator is the application-side port for a single moderation decision.
// Implementations may be remote (HTTP/SDK call to a moderation provider) or
// local (a deterministic classifier). The workflow does not branch on impl
// kind — it consumes the returned Verdict's Decision field uniformly.
type Moderator interface {
	Moderate(ctx context.Context, in ModerateInput) (safety.Verdict, error)
}
