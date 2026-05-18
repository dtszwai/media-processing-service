package audit_test

import (
	"strings"
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
)

func TestActorTypeConstants(t *testing.T) {
	cases := map[audit.ActorType]string{
		audit.ActorUser:     "USER",
		audit.ActorAPIKey:   "API_KEY",
		audit.ActorWorker:   "WORKER",
		audit.ActorOperator: "OPERATOR",
		audit.ActorSystem:   "SYSTEM",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Fatalf("ActorType drift: got %q want %q", got, want)
		}
	}
}

func TestDecisionConstants(t *testing.T) {
	// Audit decisions are a superset of safety decisions; guard the extras so
	// nobody quietly collapses them into the safety enum.
	for _, d := range []audit.Decision{
		audit.DecisionAllow,
		audit.DecisionDeny,
		audit.DecisionPass,
		audit.DecisionFail,
		audit.DecisionOverride,
		audit.DecisionReset,
		audit.DecisionReplay,
	} {
		if string(d) == "" {
			t.Fatalf("empty audit decision")
		}
	}
}

func TestEventTypeNamespace(t *testing.T) {
	// All canonical event types are dotted, lowercase, and namespaced under a
	// known family. Filters consume the family prefix, so a typo here would
	// silently drop events from dashboards.
	families := []string{"identity.", "safety.", "quota.", "workflow.",
		"idempotency.", "outbox.", "sqs.", "webhook.", "admin."}
	all := []string{
		audit.EventIdentityLoginSucceeded,
		audit.EventIdentityLoginFailed,
		audit.EventIdentityAPIKeyCreated,
		audit.EventIdentityAPIKeyRevoked,
		audit.EventSafetyInputModerationDecided,
		audit.EventSafetyOutputModerationDecided,
		audit.EventSafetyDisclosureGateDecided,
		audit.EventQuotaCapChanged,
		audit.EventWorkflowJobCancelled,
		audit.EventIdempotencyClaimReset,
		audit.EventOutboxDLQReplayed,
		audit.EventOutboxDLQAbandoned,
		audit.EventOutboxDLQDeleted,
		audit.EventOutboxDLQPurged,
		audit.EventSQSDLQReplayed,
		audit.EventWebhookSecretRotated,
		audit.EventAdminTenantSuspended,
		audit.EventAdminTenantUnsuspended,
	}
	for _, name := range all {
		if strings.ToLower(name) != name {
			t.Fatalf("event type must be lowercase: %q", name)
		}
		var matched bool
		for _, f := range families {
			if strings.HasPrefix(name, f) {
				matched = true
				break
			}
		}
		if !matched {
			t.Fatalf("event type %q not under a known family", name)
		}
	}
}
