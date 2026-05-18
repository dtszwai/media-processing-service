// Package audit defines the immutable audit-event domain. Event rows are
// append-only (written with attribute_not_exists(PK) at the storage layer) and
// carry a 1-year TTL; corrections are expressed as new rows, never updates.
// BeforeHash/AfterHash are content-addressed pointers into the state-diff
// store so the event itself stays small and PII-free.
package audit

import "time"

type ActorType string

const (
	ActorUser     ActorType = "USER"
	ActorAPIKey   ActorType = "API_KEY"
	ActorWorker   ActorType = "WORKER"
	ActorOperator ActorType = "OPERATOR"
	ActorSystem   ActorType = "SYSTEM"
)

// Decision is the audit-layer projection of a subsystem's outcome. It is
// intentionally broader than safety.Decision: audit rows record allow/deny
// authz checks, pass/fail safety gates, and operator overrides through the
// same column.
type Decision string

const (
	DecisionAllow    Decision = "ALLOW"
	DecisionDeny     Decision = "DENY"
	DecisionPass     Decision = "PASS"
	DecisionFail     Decision = "FAIL"
	DecisionOverride Decision = "OVERRIDE"
	DecisionReset    Decision = "RESET"
	DecisionReplay   Decision = "REPLAY"
)

type EntityRef struct {
	Type string
	ID   string
}

// Event is the audit row. EventType is a versioned dotted name; the canonical
// set lives in the EventType* constants below. Summary is a redacted,
// JSON-serializable bag — keep it small and never put raw inputs in it.
type Event struct {
	ID         string
	TenantID   string
	EventType  string
	ActorType  ActorType
	ActorID    string
	Entity     EntityRef
	Decision   Decision
	ReasonCode string
	Summary    map[string]any
	BeforeHash string
	AfterHash  string
	RequestID  string
	TraceID    string
	CreatedAt  time.Time
}

// Event-type constants. Versioned dotted names; bump the trailing segment
// (e.g. ".v2") when the Summary schema has a breaking change rather than
// renaming the family — consumers filter on prefix.
const (
	EventIdentityLoginSucceeded = "identity.login.succeeded"
	EventIdentityLoginFailed    = "identity.login.failed"
	EventIdentityAPIKeyCreated  = "identity.api_key.created"
	EventIdentityAPIKeyRevoked  = "identity.api_key.revoked"

	EventSafetyInputModerationDecided  = "safety.input_moderation.decided"
	EventSafetyOutputModerationDecided = "safety.output_moderation.decided"
	EventSafetyDisclosureGateDecided   = "safety.disclosure_gate.decided"

	EventQuotaCapChanged = "quota.cap.changed"

	EventWorkflowJobCancelled             = "workflow.job.cancelled"
	EventWorkflowPromptEnhancementApplied = "workflow.prompt_enhancement.applied"

	EventIdempotencyClaimReset = "idempotency.claim.reset"

	EventOutboxDLQReplayed  = "outbox.dlq.replayed"
	EventOutboxDLQAbandoned = "outbox.dlq.abandoned"
	EventOutboxDLQDeleted   = "outbox.dlq.deleted"
	EventOutboxDLQPurged    = "outbox.dlq.purged"
	EventSQSDLQReplayed     = "sqs.dlq.replayed"

	EventWebhookSecretRotated = "webhook.secret.rotated"

	EventAdminTenantSuspended   = "admin.tenant.suspended"
	EventAdminTenantUnsuspended = "admin.tenant.unsuspended"
)
