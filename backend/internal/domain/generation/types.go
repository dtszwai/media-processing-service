// Package generation defines the generation job domain.
package generation

import (
	"errors"
	"time"
)

type Status string

const (
	StatusQueued   Status = "QUEUED"
	StatusRunning  Status = "RUNNING"
	StatusBlocked  Status = "BLOCKED"
	StatusFailed   Status = "FAILED"
	StatusComplete Status = "COMPLETE"
)

type Tier string

const (
	TierFree Tier = "FREE"
	TierPaid Tier = "PAID"
)

type OutputType string

const (
	OutputImage OutputType = "IMAGE"
	OutputAudio OutputType = "AUDIO"
)

type Stage string

const (
	StageInputModeration       Stage = "INPUT_MODERATION"
	StageCostReserve           Stage = "COST_RESERVE"
	StagePromptPrepare         Stage = "PROMPT_PREPARE"
	StageProviderSubmit        Stage = "PROVIDER_SUBMIT"
	StageProviderWait          Stage = "PROVIDER_WAIT"
	StageOutputModeration      Stage = "OUTPUT_MODERATION"
	StageDisclosurePostprocess Stage = "DISCLOSURE_POSTPROCESS"
	StagePublish               Stage = "PUBLISH"
	StageTerminal              Stage = "TERMINAL"
)

type ResourceClass string

const (
	ResourceFast         ResourceClass = "FAST"
	ResourceProvider     ResourceClass = "PROVIDER"
	ResourcePoll         ResourceClass = "POLL"
	ResourceImageProcess ResourceClass = "IMAGE_PROCESS"
)

// Job is the generation aggregate.
type Job struct {
	ID                   string
	TenantID             string
	UserID               string
	MediaID              string
	ResultAssetID        string
	OutputType           OutputType
	Tier                 Tier
	Status               Status
	CurrentStage         Stage
	StageVersion         uint64
	Provider             string
	Model                string
	Resolution           string
	Seed                 int64
	VariantCount         int
	Prompt               string
	PreparedPrompt       string
	PreparedPromptHash   string
	PromptSpecVersion    string
	GenerationParamsHash string
	Attempts             int
	ProviderJobID        string
	ProviderRequestID    string
	// BudgetDate and BudgetMicroUSD are set at COST_RESERVE and carried
	// through to later stages so the ledger Commit/Release ops have the
	// correct partition key and amount without re-deriving them.
	BudgetDate     string
	BudgetMicroUSD int64
	Error          *Error
	CreatedAt      time.Time
	UpdatedAt      time.Time
	CompletedAt    *time.Time
}

// Error categorizes a stage failure for retry and terminal classification.
type Error struct {
	Code       string
	Message    string
	Terminal   bool
	RetryAfter time.Duration
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

// Terminal returns a non-retryable error.
func Terminal(code, msg string) error {
	return &Error{Code: code, Message: msg, Terminal: true}
}

// Transient returns a retryable error.
func Transient(code, msg string) error {
	return &Error{Code: code, Message: msg, Terminal: false}
}

// AsError narrows err to *Error when possible.
func AsError(err error) *Error {
	if err == nil {
		return nil
	}
	if ge, ok := errors.AsType[*Error](err); ok {
		return ge
	}
	return &Error{Code: "UNKNOWN_ERROR", Message: err.Error(), Terminal: false}
}

// IsTerminal reports whether err should stop the workflow.
func IsTerminal(err error) bool {
	if err == nil {
		return false
	}
	ge := AsError(err)
	return ge != nil && ge.Terminal
}

// PollStatus is the result of an async provider poll.
type PollStatus string

const (
	PollPending PollStatus = "PENDING"
	PollReady   PollStatus = "READY"
	PollFailed  PollStatus = "FAILED"
)

// JobSpec is the provider-facing job description.
type JobSpec struct {
	JobID           string
	MediaID         string
	TenantID        string
	OutputType      OutputType
	Provider        string
	Prompt          string
	Model           string
	Resolution      string
	Seed            int64
	ClientRequestID string
	Metadata        map[string]string
}

// Artifact is the provider's output for a sync generation.
type Artifact struct {
	Bytes       []byte
	ContentType string
	Extension   string
	SHA256      string
	Metadata    map[string]string
}
