package safety

import (
	"context"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

// SimulatedSentinel is the substring that triggers a FAIL verdict from the
// SimulatedModerator. Exported so tests and integration harnesses can embed
// it in fixtures without redefining the magic value. A real moderation
// provider replaces this whole impl, not just the sentinel.
const SimulatedSentinel = "__moderation_block__"

// SimulatedReviewSentinel forces a REVIEW verdict. Useful for exercising the
// REVIEW branch from a fixture without standing up a tenant-policy table.
const SimulatedReviewSentinel = "__moderation_review__"

// SimulatedModerator is a deterministic, in-process classifier used until a
// real moderation provider is wired. It returns FAIL when the prompt (or any
// artifact metadata value) contains SimulatedSentinel; otherwise it returns
// PASS. The sentinel-based design makes the fail path observable in tests
// without coupling the test surface to a probabilistic classifier.
//
// The impl lives in app/safety (not infra/moderation) because it makes no
// outbound calls — adding an HTTP/SDK-backed Moderator later would move the
// impl into infra and leave this one for tests only.
type SimulatedModerator struct {
	// Now sources the Verdict.CreatedAt timestamp. Left as a function so
	// tests can pin it without monkey-patching time.Now.
	Now func() time.Time
	// NewID mints Verdict.ID values. Defaults to randid.New; tests override
	// to assert exact ids.
	NewID func() string
	// PolicyVersion is recorded on every returned Verdict so audit rows can
	// pin the in-effect ruleset. Defaults to "simulated-v1".
	PolicyVersion string
}

// NewSimulatedModerator constructs a SimulatedModerator with safe defaults.
// Callers that need to override Now/NewID/PolicyVersion mutate the returned
// pointer's fields directly.
func NewSimulatedModerator() *SimulatedModerator {
	return &SimulatedModerator{
		Now:           func() time.Time { return time.Now().UTC() },
		NewID:         randid.New,
		PolicyVersion: "simulated-v1",
	}
}

// Moderate runs the in-process classifier. Subject framing depends on the
// layer the workflow attached to the input — Prompt for INPUT_MODERATION,
// Artifact for OUTPUT_MODERATION — so the same impl serves both gates.
func (m *SimulatedModerator) Moderate(_ context.Context, in ModerateInput) (safety.Verdict, error) {
	subjectKind, subjectID, decision, reason := classify(in)
	v := safety.Verdict{
		ID:            m.newID(),
		TenantID:      in.TenantID,
		SubjectType:   subjectKind,
		SubjectID:     subjectID,
		Layer:         in.Layer,
		Decision:      decision,
		PolicyVersion: m.policyVersion(),
		Provider:      "simulated",
		Model:         "simulated-v1",
		ReasonCode:    reason,
		CreatedAt:     m.now(),
	}
	return v, nil
}

// classify extracts the moderation subject from the input and runs the
// local rule against it. Keeping the rule in a single place lets the
// review/fail sentinels stay symmetrical across the two layers.
func classify(in ModerateInput) (subjectKind, subjectID string, decision safety.Decision, reason string) {
	if in.Artifact != nil {
		decision, reason = evaluateArtifact(in.Artifact)
		return "ARTIFACT", in.Artifact.SHA256, decision, reason
	}
	decision, reason = evaluateText(in.Prompt)
	return "PROMPT", in.JobID, decision, reason
}

func evaluateText(s string) (safety.Decision, string) {
	if strings.Contains(s, SimulatedSentinel) {
		return safety.DecisionFail, "MODERATION_SENTINEL_BLOCK"
	}
	if strings.Contains(s, SimulatedReviewSentinel) {
		return safety.DecisionReview, "MODERATION_SENTINEL_REVIEW"
	}
	return safety.DecisionPass, ""
}

// evaluateArtifact inspects artifact metadata for the sentinel. Real impls
// would decode bytes, run image/audio safety classifiers, and OCR for text;
// the simulated path is a metadata flag so unit tests stay deterministic.
func evaluateArtifact(a *generation.Artifact) (safety.Decision, string) {
	if a == nil {
		return safety.DecisionPass, ""
	}
	for _, v := range a.Metadata {
		if strings.Contains(v, SimulatedSentinel) {
			return safety.DecisionFail, "MODERATION_SENTINEL_BLOCK"
		}
		if strings.Contains(v, SimulatedReviewSentinel) {
			return safety.DecisionReview, "MODERATION_SENTINEL_REVIEW"
		}
	}
	return safety.DecisionPass, ""
}

func (m *SimulatedModerator) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now().UTC()
}

func (m *SimulatedModerator) newID() string {
	if m.NewID != nil {
		return m.NewID()
	}
	return randid.New()
}

func (m *SimulatedModerator) policyVersion() string {
	if m.PolicyVersion != "" {
		return m.PolicyVersion
	}
	return "simulated-v1"
}
