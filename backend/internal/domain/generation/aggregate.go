package generation

import "time"

// Status is reused across Job (the workflow control aggregate), and the
// user-facing Generation/Output/Variant aggregates because they share the same
// observable lifecycle vocabulary. Diverging the enums would force every
// transport-layer projection to translate between near-identical sets.
const (
	StatusCancelled Status = "CANCELLED"
)

// GenerationMode disambiguates the three lifecycle entries into the pipeline.
// CREATE is a fresh request, EDIT chains off a prior Variant/Output via Edit,
// and RERUN re-executes a prior Generation with the same SpecSummary (used for
// transient failures or operator-triggered replays).
type GenerationMode string

const (
	GenerationModeCreate GenerationMode = "CREATE"
	GenerationModeEdit   GenerationMode = "EDIT"
	GenerationModeRerun  GenerationMode = "RERUN"
)

// Resolution is intentionally a value type — generation specs travel through
// many layers and a pointer-to-resolution adds noise without giving the
// generation aggregate anything mutable.
type Resolution struct {
	Width  int
	Height int
}

// SpecSummary is the redacted, non-secret projection of a generation request.
// It is safe to log, attach to audit rows, and surface in webhook payloads;
// the full raw spec (prompts, edit masks, etc.) is held elsewhere encrypted.
type SpecSummary struct {
	OutputType   OutputType
	Provider     string
	Model        string
	Resolution   *Resolution
	DurationMS   int64
	Voice        string
	Seed         string
	VariantCount int
	Tier         string
}

// Generation is the user-facing aggregate that anchors a request. It is the
// stable identity exposed in the API, while Job remains the internal workflow
// state machine. One Generation owns 0..1 Outputs (current attempt) plus
// historical Outputs from prior RERUNs, and SourceEditID links it to the Edit
// that derived this request from a prior Variant.
type Generation struct {
	ID                  string
	TenantID            string
	MediaID             string
	CreatedByUserID     string
	APIKeyID            string
	OutputType          OutputType
	Mode                GenerationMode
	Status              Status
	ActiveJobID         string
	PrimaryOutputID     string
	SourceEditID        string
	RequestHash         string
	SpecSummary         SpecSummary
	PricingVersion      string
	SafetyPolicyVersion string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	CompletedAt         *time.Time
}

// Output is the per-attempt fanout container. A single Generation has at most
// one in-flight Output; a RERUN creates a fresh Output rather than mutating the
// previous one so that variant history stays addressable.
type Output struct {
	ID                    string
	TenantID              string
	MediaID               string
	GenerationID          string
	JobID                 string
	Type                  OutputType
	Status                Status
	VariantCountRequested int
	VariantCountCompleted int
	DefaultVariantID      string
	SafetySummary         map[string]any
	CostSummary           map[string]any
	CreatedAt             time.Time
	UpdatedAt             time.Time
	CompletedAt           *time.Time
}

// Variant is a single concrete provider output (one image, one audio file, …).
// FinalAssetID is set only after the disclosure gate passes; StagedArtifactID
// references the pre-gate artifact, kept separately so the gate can fail
// without polluting the canonical media surface.
type Variant struct {
	ID                        string
	TenantID                  string
	MediaID                   string
	OutputID                  string
	GenerationID              string
	JobID                     string
	Index                     int
	Status                    Status
	FinalAssetID              string
	StagedArtifactID          string
	Provider                  string
	Model                     string
	Seed                      string
	MIME                      string
	Bytes                     uint64
	ProviderRequestID         string
	SafetyCaseID              string
	Watermark                 map[string]any
	ProvenanceManifestAssetID string
	Score                     *float64
	Error                     *VariantError
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	CompletedAt               *time.Time
}

// VariantError is intentionally distinct from domain/generation.Error: the
// workflow Error type carries retry semantics consumed by the FSM, while a
// VariantError is the terminal projection persisted on the variant row.
type VariantError struct {
	Code    string
	Message string
}

// Edit is a lineage edge, not an aggregate. It records the relationship
// between a prior Variant/Output and the new Generation it spawned, plus the
// billing class so downstream cost rollups can discount edits without
// reverse-engineering provider receipts.
type Edit struct {
	ID                 string
	TenantID           string
	SourceMediaID      string
	SourceAssetID      string
	SourceOutputID     string
	SourceVariantID    string
	TargetMediaID      string
	TargetGenerationID string
	Type               EditType
	PromptDeltaHash    string
	BillingClass       EditBillingClass
	CreatedAt          time.Time
}

type EditType string

const (
	EditTypePromptDelta   EditType = "PROMPT_DELTA"
	EditTypeInpaint       EditType = "INPAINT"
	EditTypeOutpaint      EditType = "OUTPAINT"
	EditTypeAudioRevise   EditType = "AUDIO_REVISE"
	EditTypeStyleTransfer EditType = "STYLE_TRANSFER"
)

type EditBillingClass string

const (
	EditBillingFull      EditBillingClass = "FULL_GENERATION"
	EditBillingDiscount  EditBillingClass = "EDIT_DISCOUNT"
	EditBillingFreeRetry EditBillingClass = "FREE_RETRY"
)
