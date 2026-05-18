// Package safety defines the AI-safety domain types. Verdicts are the
// immutable evidence rows persisted by the input/output moderation and
// disclosure-gate layers; the gate decision is split out because the
// disclosure gate composes signals from multiple verdicts and emits a single
// pass/fail per output type.
package safety

import "time"

type Decision string

const (
	DecisionPass   Decision = "PASS"
	DecisionFail   Decision = "FAIL"
	DecisionReview Decision = "REVIEW"
)

type Layer string

const (
	LayerInputModeration  Layer = "INPUT_MODERATION"
	LayerOutputModeration Layer = "OUTPUT_MODERATION"
	LayerDisclosureGate   Layer = "DISCLOSURE_GATE"
)

// Verdict is a single moderation decision attributed to a subject (media,
// asset, variant, prompt). EvidenceHashes are content-addressed pointers into
// the safety-case store so the original signals can be re-fetched without
// duplicating PII into the audit trail.
type Verdict struct {
	ID             string
	TenantID       string
	SubjectType    string
	SubjectID      string
	Layer          Layer
	Decision       Decision
	Categories     map[string]float64
	PolicyVersion  string
	Provider       string
	Model          string
	EvidenceHashes []string
	ReasonCode     string
	CreatedAt      time.Time
}

// DisclosureGateDecision is the per-output-type gate row. It is not a Verdict
// because the gate's job is to compose Verdicts and provenance signals, not to
// emit a new moderation signal of its own. ErrorCode is set only when
// Decision == DecisionFail; a passing gate has all four *Present flags true.
type DisclosureGateDecision struct {
	OutputType        string
	Decision          Decision
	GateVersion       string
	DisclosurePresent bool
	WatermarkPresent  bool
	SafetyPresent     bool
	ManifestPresent   bool
	ErrorCode         string
	CreatedAt         time.Time
}
