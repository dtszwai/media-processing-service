package audit_test

import (
	"testing"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
)

// TestEventConstructors locks the wired audit-event constructors against
// signature drift. Each case asserts the canonical (event_type, actor,
// entity-id) tuple so a renamed actor type or moved entity id surfaces
// immediately instead of as a silent GSI-partition shift.
//
// Summary contents are deliberately NOT asserted — they are documentation
// fields that evolve with the emitter.
func TestEventConstructors(t *testing.T) {
	cases := []struct {
		name      string
		ev        audit.Event
		eventType string
		actor     audit.ActorType
		entityID  string
	}{
		{
			name:      "QuotaCapChanged",
			ev:        auditapp.NewQuotaCapChanged("op-1", "TENANT", "tenant-1", "COST_MICRO_USD", "20260515", 100, 200, "req-3"),
			eventType: audit.EventQuotaCapChanged,
			actor:     audit.ActorOperator,
			entityID:  "TENANT#tenant-1#COST_MICRO_USD#20260515",
		},
		{
			name:      "WorkflowJobCancelled",
			ev:        auditapp.NewWorkflowJobCancelled(audit.ActorUser, "user-1", "tenant-1", "job-99", "USER_CANCELLED", "req-5"),
			eventType: audit.EventWorkflowJobCancelled,
			actor:     audit.ActorUser,
			entityID:  "job-99",
		},
		{
			name:      "WorkflowPromptEnhancementApplied",
			ev:        auditapp.NewWorkflowPromptEnhancementApplied("tenant-1", "job-99", true, "enh_1", "policy-v1", "openai", "gpt-test", "IMAGE", 10, 20),
			eventType: audit.EventWorkflowPromptEnhancementApplied,
			actor:     audit.ActorSystem,
			entityID:  "job-99",
		},
		{
			name:      "IdempotencyClaimReset",
			ev:        auditapp.NewIdempotencyClaimReset("op-1", "GEN#tenant-1#hash-abc", 1, 2, "STUCK_CLAIM_OPERATOR_OVERRIDE", "req-7"),
			eventType: audit.EventIdempotencyClaimReset,
			actor:     audit.ActorOperator,
			entityID:  "GEN#tenant-1#hash-abc",
		},
		{
			name:      "WebhookSecretRotated",
			ev:        auditapp.NewWebhookSecretRotated("user-1", "tenant-1", "wh-1", "key-old", "key-new", "req-10"),
			eventType: audit.EventWebhookSecretRotated,
			actor:     audit.ActorUser,
			entityID:  "wh-1",
		},
		{
			name:      "AdminTenantSuspended",
			ev:        auditapp.NewAdminTenantSuspended("op-1", "tenant-1", "ABUSE_DETECTED", "req-11"),
			eventType: audit.EventAdminTenantSuspended,
			actor:     audit.ActorOperator,
			entityID:  "tenant-1",
		},
		{
			name:      "AdminTenantUnsuspended",
			ev:        auditapp.NewAdminTenantUnsuspended("op-1", "tenant-1", "REVIEW_CLEARED", "req-12"),
			eventType: audit.EventAdminTenantUnsuspended,
			actor:     audit.ActorOperator,
			entityID:  "tenant-1",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ev.EventType; got != tc.eventType {
				t.Errorf("EventType = %q, want %q", got, tc.eventType)
			}
			if got := tc.ev.ActorType; got != tc.actor {
				t.Errorf("ActorType = %q, want %q", got, tc.actor)
			}
			if tc.ev.Entity.ID == "" {
				t.Errorf("Entity.ID empty — every audit row must address an entity")
			}
			if got := tc.ev.Entity.ID; got != tc.entityID {
				t.Errorf("Entity.ID = %q, want %q", got, tc.entityID)
			}
		})
	}
}
