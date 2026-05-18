package ops

import "time"

// JobSummary is the operator's row-level projection of a Job. The full FSM
// state lives on FullJobView; JobSummary stays cheap so the jobs/library tabs
// can paint thousands of rows without joining cross-PK tables.
type JobSummary struct {
	JobID        string
	TenantID     string
	MediaID      string
	Status       string
	CurrentStage string
	OutputType   string
	Tier         string
	Model        string
	Attempts     int32
	ErrorCode    string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	CompletedAt  *time.Time
}

// ListJobsFilter narrows the table scan. Empty values disable the filter.
type ListJobsFilter struct {
	TenantID   string
	Status     string
	OutputType string
	Limit      int32
	Cursor     string
}

// FullJobView is what the trace tab consumes. The frontend renders without
// further joins; everything addressable from the PK=JOB#<id> partition plus
// the linked Media / Asset / Gate rows is bundled here.
type FullJobView struct {
	Summary      JobSummary
	JobAttrs     map[string]any
	MediaAttrs   map[string]any
	AssetAttrs   map[string]any
	Spans        []TraceSpan
	GateDecision *GateDecisionView
	RelatedKeys  []string
	FirstEventAt time.Time
	LastEventAt  time.Time
	// DecryptedPrompt is the plaintext prompt the customer submitted,
	// unsealed via the same KMS Sealer the worker uses. Populated only
	// when the JOB row carries an encrypted_prompt blob and the Service
	// has a JobRepo with a sealer attached; otherwise empty.
	DecryptedPrompt         string
	DecryptedPreparedPrompt string
}

// TraceSpan flatly represents every observable event under a job. Stage rows
// are the parents; ATTEMPTs, PROVIDER_REQUESTs, gate audits, terminal status
// rows hang under them. Sorting by start_at ascending is the rendering
// invariant.
type TraceSpan struct {
	ID            string
	ParentID      string
	Kind          string
	Label         string
	Status        string
	Stage         string
	ResourceClass string
	AttemptNo     int32
	ErrorCode     string
	ErrorMessage  string
	Attributes    map[string]string
	StartAt       time.Time
	EndAt         time.Time
	DurationMS    int64
	// PK / SK identify the backing DDB row when the span is 1:1 with one.
	// Synthetic stage rollups have neither; everything else populates both
	// so the console can render the span id as a deep link to the DDB inspector.
	PK string
	SK string
}

// GateDecisionView is the typed projection of the AUDIT#GATE#<jobID> row
// plus the watermark fingerprint pulled from S3 object metadata. The
// fingerprint is the SHA-256 of the stamped image bytes — the gate audit
// row carries only a "present" bool, so the operator's verification surface
// reads through to S3 for the actual 64-char hex.
type GateDecisionView struct {
	JobID                string
	TenantID             string
	GateVersion          string
	OutputType           string
	Provider             string
	Model                string
	Decision             string
	ErrorCode            string
	WatermarkPresent     bool
	DisclosurePresent    bool
	SafetyPresent        bool
	WatermarkFingerprint string
	WatermarkAlgo        string
	WatermarkPosition    string
	WatermarkText        string
	DecidedAt            time.Time
}
