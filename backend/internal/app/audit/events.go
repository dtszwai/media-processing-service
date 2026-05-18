package audit

import (
	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
)

// Constructor helpers keep call sites declarative: callers express what
// happened, not how the row is laid out. Each helper fills the canonical
// (event_type, actor, entity, decision) tuple for one family. CreatedAt
// and ID are intentionally left zero — the Recorder fills them so the
// monotonic ordering on PK/SK reflects write order rather than caller
// clocks (which may skew between API replicas).
//
// Summary slots only carry redacted, non-secret structured data; raw
// prompts, secrets, and PII never appear here.

// NewIdentityLoginSucceeded records a successful login. ActorID is the
// resolved user_id so the GSI on ACTOR#USER#<id> threads logins together.
func NewIdentityLoginSucceeded(userID, tenantID, requestID string) audit.Event {
	return audit.Event{
		TenantID:  tenantID,
		EventType: audit.EventIdentityLoginSucceeded,
		ActorType: audit.ActorUser,
		ActorID:   userID,
		Entity:    audit.EntityRef{Type: "USER", ID: userID},
		Decision:  audit.DecisionAllow,
		RequestID: requestID,
	}
}

// NewIdentityLoginFailed records a failed login attempt. The actor id is
// intentionally the submitted email — a successful resolution would have
// emitted Succeeded; the email is what an operator searches by.
func NewIdentityLoginFailed(email, reasonCode, requestID string) audit.Event {
	return audit.Event{
		EventType:  audit.EventIdentityLoginFailed,
		ActorType:  audit.ActorUser,
		ActorID:    email,
		Entity:     audit.EntityRef{Type: "USER", ID: email},
		Decision:   audit.DecisionDeny,
		ReasonCode: reasonCode,
		RequestID:  requestID,
	}
}

// NewAPIKeyCreated records an API-key issuance. The Summary captures the
// key's display name; the raw key material never appears here.
func NewAPIKeyCreated(tenantID, actorUserID, keyID, keyName, requestID string) audit.Event {
	return audit.Event{
		TenantID:  tenantID,
		EventType: audit.EventIdentityAPIKeyCreated,
		ActorType: audit.ActorUser,
		ActorID:   actorUserID,
		Entity:    audit.EntityRef{Type: "API_KEY", ID: keyID},
		Decision:  audit.DecisionAllow,
		Summary:   map[string]any{"name": keyName},
		RequestID: requestID,
	}
}

// NewAPIKeyRevoked records an API-key revocation.
func NewAPIKeyRevoked(tenantID, actorUserID, keyID, requestID string) audit.Event {
	return audit.Event{
		TenantID:  tenantID,
		EventType: audit.EventIdentityAPIKeyRevoked,
		ActorType: audit.ActorUser,
		ActorID:   actorUserID,
		Entity:    audit.EntityRef{Type: "API_KEY", ID: keyID},
		Decision:  audit.DecisionAllow,
		RequestID: requestID,
	}
}

// NewOutboxDLQReplayed records an operator-initiated replay of one outbox
// DLQ message. dlqName / originalMessageID / newMessageID are not secrets
// and survive in the Summary so audit consumers can reconcile replays
// against the source queue.
func NewOutboxDLQReplayed(tenantID, actorUserID, dlqName, originalMessageID, newMessageID, requestID string) audit.Event {
	return audit.Event{
		TenantID:  tenantID,
		EventType: audit.EventOutboxDLQReplayed,
		ActorType: audit.ActorOperator,
		ActorID:   actorUserID,
		Entity:    audit.EntityRef{Type: "DLQ_MESSAGE", ID: originalMessageID},
		Decision:  audit.DecisionReplay,
		Summary: map[string]any{
			"dlq_name":            dlqName,
			"original_message_id": originalMessageID,
			"new_message_id":      newMessageID,
		},
		RequestID: requestID,
	}
}

// NewOutboxDLQAbandoned records an operator decision to leave a poisoned
// outbox message out of the replay path while preserving the row for review.
func NewOutboxDLQAbandoned(tenantID, actorUserID, dlqName, messageID, reasonCode, requestID string) audit.Event {
	return audit.Event{
		TenantID:   tenantID,
		EventType:  audit.EventOutboxDLQAbandoned,
		ActorType:  audit.ActorOperator,
		ActorID:    actorUserID,
		Entity:     audit.EntityRef{Type: "DLQ_MESSAGE", ID: messageID},
		Decision:   audit.DecisionDeny,
		ReasonCode: reasonCode,
		Summary: map[string]any{
			"dlq_name":   dlqName,
			"message_id": messageID,
		},
		RequestID: requestID,
	}
}

// NewSQSDLQReplayed records replay of an SQS-shaped DLQ message. Split from
// NewOutboxDLQReplayed even though the shape coincides today — the two
// streams have different downstream consumers and audit dashboards filter
// on the event_type prefix to separate them.
func NewSQSDLQReplayed(tenantID, actorUserID, dlqName, originalMessageID, newMessageID, requestID string) audit.Event {
	return audit.Event{
		TenantID:  tenantID,
		EventType: audit.EventSQSDLQReplayed,
		ActorType: audit.ActorOperator,
		ActorID:   actorUserID,
		Entity:    audit.EntityRef{Type: "DLQ_MESSAGE", ID: originalMessageID},
		Decision:  audit.DecisionReplay,
		Summary: map[string]any{
			"dlq_name":            dlqName,
			"original_message_id": originalMessageID,
			"new_message_id":      newMessageID,
		},
		RequestID: requestID,
	}
}

// NewSafetyInputModerationDecided records the verdict of INPUT_MODERATION
// on a generation job. Runs BEFORE cost reservation, so the audit row is
// the canonical accountability record for prompts the platform refused to
// hand to a provider; the budget ledger never observes them. SubjectID is
// the job id (the prompt itself is encrypted at rest and excluded here).
func NewSafetyInputModerationDecided(tenantID, jobID, decisionStr, reasonCode, policyVersion, provider, model string) audit.Event {
	return audit.Event{
		TenantID:   tenantID,
		EventType:  audit.EventSafetyInputModerationDecided,
		ActorType:  audit.ActorSystem,
		ActorID:    provider,
		Entity:     audit.EntityRef{Type: "GENERATION_JOB", ID: jobID},
		Decision:   moderationDecision(decisionStr),
		ReasonCode: reasonCode,
		Summary: map[string]any{
			"layer":          "INPUT_MODERATION",
			"policy_version": policyVersion,
			"provider":       provider,
			"model":          model,
		},
	}
}

// NewSafetyOutputModerationDecided records the verdict of OUTPUT_MODERATION
// against a staged artifact. Runs after provider work but BEFORE the
// disclosure pipeline mutates customer-visible bytes, so a FAIL here means
// the artifact never reaches the final sink. ArtifactHash is the content-
// addressed reference to the safety-case store; raw bytes never live here.
func NewSafetyOutputModerationDecided(tenantID, jobID, decisionStr, reasonCode, policyVersion, provider, model, artifactHash string) audit.Event {
	return audit.Event{
		TenantID:   tenantID,
		EventType:  audit.EventSafetyOutputModerationDecided,
		ActorType:  audit.ActorSystem,
		ActorID:    provider,
		Entity:     audit.EntityRef{Type: "GENERATION_JOB", ID: jobID},
		Decision:   moderationDecision(decisionStr),
		ReasonCode: reasonCode,
		Summary: map[string]any{
			"layer":          "OUTPUT_MODERATION",
			"policy_version": policyVersion,
			"provider":       provider,
			"model":          model,
			"artifact_hash":  artifactHash,
		},
	}
}

// moderationDecision maps a domain/safety.Decision string onto the audit
// Decision vocabulary. PASS/FAIL/REVIEW all surface as distinguishable
// audit Decisions so operators can filter on the audit row without joining
// to the per-verdict store.
func moderationDecision(decisionStr string) audit.Decision {
	switch decisionStr {
	case "PASS":
		return audit.DecisionPass
	case "REVIEW":
		// REVIEW does not have its own audit.Decision constant — it
		// projects onto DENY so operator dashboards filtering on "denied"
		// queries still surface manual-review holds.
		return audit.DecisionDeny
	default:
		return audit.DecisionFail
	}
}

// NewSafetyDisclosureGateDecided wraps the AI-disclosure gate decision so
// the standalone Recorder receives the same Event shape callers use for
// auth/admin events. The Summary carries the gate-version + per-control
// presence flags that the per-job row also persists; duplicating them here
// keeps audit-wide queries useful without forcing readers to join.
func NewSafetyDisclosureGateDecided(tenantID, jobID, decisionStr, errorCode, gateVersion, provider, model string, watermark, disclosure, safety bool) audit.Event {
	d := audit.DecisionPass
	if decisionStr != "PASS" {
		d = audit.DecisionFail
	}
	return audit.Event{
		TenantID:   tenantID,
		EventType:  audit.EventSafetyDisclosureGateDecided,
		ActorType:  audit.ActorSystem,
		ActorID:    provider,
		Entity:     audit.EntityRef{Type: "GENERATION_JOB", ID: jobID},
		Decision:   d,
		ReasonCode: errorCode,
		Summary: map[string]any{
			"gate_version":       gateVersion,
			"provider":           provider,
			"model":              model,
			"watermark_present":  watermark,
			"disclosure_present": disclosure,
			"safety_present":     safety,
		},
	}
}

// NewQuotaCapChanged records an operator-driven cap rotation against the
// generic Reservoir taxonomy (scope, scope id, metric, period). Old/new
// caps are captured as int64 micro-units of the metric so downstream
// dashboards can plot the rotation without re-deriving units.
func NewQuotaCapChanged(operatorID, scopeType, scopeID, metric, period string, oldCap, newCap int64, requestID string) audit.Event {
	return audit.Event{
		EventType: audit.EventQuotaCapChanged,
		ActorType: audit.ActorOperator,
		ActorID:   operatorID,
		Entity:    audit.EntityRef{Type: "RESERVOIR", ID: scopeType + "#" + scopeID + "#" + metric + "#" + period},
		Decision:  audit.DecisionAllow,
		Summary: map[string]any{
			"scope_type": scopeType,
			"scope_id":   scopeID,
			"metric":     metric,
			"period":     period,
			"old_cap":    oldCap,
			"new_cap":    newCap,
		},
		RequestID: requestID,
	}
}

// NewWorkflowJobCancelled records a user-initiated or operator-initiated
// generation job cancellation. Reason distinguishes user-cancel from
// admin-cancel from auto-cancel (e.g. tenant suspension) so the analytics
// rollup can break cancellation reasons out without parsing free text.
func NewWorkflowJobCancelled(actorType audit.ActorType, actorID, tenantID, jobID, reasonCode, requestID string) audit.Event {
	return audit.Event{
		TenantID:   tenantID,
		EventType:  audit.EventWorkflowJobCancelled,
		ActorType:  actorType,
		ActorID:    actorID,
		Entity:     audit.EntityRef{Type: "GENERATION_JOB", ID: jobID},
		Decision:   audit.DecisionAllow,
		ReasonCode: reasonCode,
		RequestID:  requestID,
	}
}

// NewWorkflowPromptEnhancementApplied records the PROMPT_PREPARE stage's
// enhancement outcome. Raw prompts never appear in Summary; hashes and refs
// are internal correlation handles only.
func NewWorkflowPromptEnhancementApplied(tenantID, jobID string, applied bool, ref, policyVersion, provider, model, outputType string, tokensIn, tokensOut int64) audit.Event {
	return audit.Event{
		ID:        "prompt-enhancement#" + jobID + "#" + ref,
		TenantID:  tenantID,
		EventType: audit.EventWorkflowPromptEnhancementApplied,
		ActorType: audit.ActorSystem,
		ActorID:   provider,
		Entity:    audit.EntityRef{Type: "GENERATION_JOB", ID: jobID},
		Decision:  audit.DecisionAllow,
		Summary: map[string]any{
			"applied":        applied,
			"ref":            ref,
			"policy_version": policyVersion,
			"provider":       provider,
			"model":          model,
			"output_type":    outputType,
			"tokens_in":      tokensIn,
			"tokens_out":     tokensOut,
		},
	}
}

// NewIdempotencyClaimReset records an operator-initiated reset of an
// idempotency claim that is otherwise terminal-for-14-days. Old/new
// generations are int because the claim-scope versioning scheme uses a
// monotonic generation counter. Scope is the composite claim key (without the
// secret prompt material) so operators can correlate the reset against the
// original failure.
func NewIdempotencyClaimReset(operatorID, scope string, oldGeneration, newGeneration int, reasonCode, requestID string) audit.Event {
	return audit.Event{
		EventType:  audit.EventIdempotencyClaimReset,
		ActorType:  audit.ActorOperator,
		ActorID:    operatorID,
		Entity:     audit.EntityRef{Type: "IDEMPOTENCY_CLAIM", ID: scope},
		Decision:   audit.DecisionReset,
		ReasonCode: reasonCode,
		Summary: map[string]any{
			"scope":          scope,
			"old_generation": oldGeneration,
			"new_generation": newGeneration,
		},
		RequestID: requestID,
	}
}

// NewWebhookSecretRotated records a webhook signing-secret rotation. Only
// the key ids appear — neither the old nor the new secret material crosses
// this boundary. The two key ids let operators correlate the rotation
// against signature-verification failures observed during the cutover.
func NewWebhookSecretRotated(actorUserID, tenantID, endpointID, oldSecretKeyID, newSecretKeyID, requestID string) audit.Event {
	return audit.Event{
		TenantID:  tenantID,
		EventType: audit.EventWebhookSecretRotated,
		ActorType: audit.ActorUser,
		ActorID:   actorUserID,
		Entity:    audit.EntityRef{Type: "WEBHOOK_ENDPOINT", ID: endpointID},
		Decision:  audit.DecisionAllow,
		Summary: map[string]any{
			"old_secret_key_id": oldSecretKeyID,
			"new_secret_key_id": newSecretKeyID,
		},
		RequestID: requestID,
	}
}

// NewAdminTenantSuspended records an operator-driven tenant suspension.
// Tenant-suspended is itself a write-gating state on every tenant-scoped
// write path; the audit row is the canonical "when was this flipped and by
// whom" record so reinstatement reviews don't need to mine application
// logs.
func NewAdminTenantSuspended(operatorID, tenantID, reasonCode, requestID string) audit.Event {
	return audit.Event{
		TenantID:   tenantID,
		EventType:  audit.EventAdminTenantSuspended,
		ActorType:  audit.ActorOperator,
		ActorID:    operatorID,
		Entity:     audit.EntityRef{Type: "TENANT", ID: tenantID},
		Decision:   audit.DecisionDeny,
		ReasonCode: reasonCode,
		RequestID:  requestID,
	}
}

// NewAdminTenantUnsuspended records lifting of a tenant suspension. Paired
// with NewAdminTenantSuspended; the two events fully bracket the
// suspended-window state on the tenant so an operator can derive the
// effective window without scanning intermediate state.
func NewAdminTenantUnsuspended(operatorID, tenantID, reasonCode, requestID string) audit.Event {
	return audit.Event{
		TenantID:   tenantID,
		EventType:  audit.EventAdminTenantUnsuspended,
		ActorType:  audit.ActorOperator,
		ActorID:    operatorID,
		Entity:     audit.EntityRef{Type: "TENANT", ID: tenantID},
		Decision:   audit.DecisionAllow,
		ReasonCode: reasonCode,
		RequestID:  requestID,
	}
}
