package outbox

import (
	"errors"
	"fmt"

	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// ErrRoutingPolicyFailed signals that a row's semantic fields can't be mapped
// to a valid SNS message-attribute set under the configured policy. Producers
// must not be trusted to supply SNS attributes directly — one typo silently
// drops every matching message behind the topic's filter policy, with no
// metric to alert on. The relay derives attributes on read and validates each
// one against the allowed enum so a misrouted row surfaces as a DLQ entry
// instead of a black-hole publish.
var ErrRoutingPolicyFailed = errors.New("outbox: routing policy failed")

// RoutingFailure annotates a routing-policy rejection with the failing field
// and value so the DLQ row carries actionable detail.
type RoutingFailure struct {
	Stream string
	Reason string
	Field  string
	Value  string
}

func (e *RoutingFailure) Error() string {
	return fmt.Sprintf("outbox: routing policy rejected %s row: %s (%s=%q)", e.Stream, e.Reason, e.Field, e.Value)
}

func (e *RoutingFailure) Unwrap() error { return ErrRoutingPolicyFailed }

// RoutingPolicy derives SNS message attributes from an outbox row's semantic
// fields. Each stream has a closed set of allowed values; anything outside
// the enum returns *RoutingFailure so the relay can DLQ the row.
type RoutingPolicy interface {
	AttributesFor(row PendingRow) (map[string]string, error)
}

// PendingRow is the subset of an outbox row the policy needs. Mirrors the
// persisted fields the relay decodes off DynamoDB. Kept narrow on purpose so
// the policy doesn't grow a dependency on the kv layer.
type PendingRow struct {
	Stream        string
	EventType     string
	TenantID      string
	TenantLane    string
	Tier          string
	Stage         string
	ResourceClass string
}

// DefaultPolicy is the static routing policy. Stream names are the same
// constants producers use, so a mismatch (e.g. a future stream not added
// here) trips the policy at the first publish attempt.
type DefaultPolicy struct{}

// AttributesFor maps a row to its SNS attributes. The attribute keys are
// load-bearing: SNS subscription filter policies on the consuming queues
// match against them letter-for-letter, so the policy never invents a key
// without a matching subscription.
func (DefaultPolicy) AttributesFor(row PendingRow) (map[string]string, error) {
	switch row.Stream {
	case StreamMedia, StreamMediaCleanup:
		if !mediaEventTypes[row.EventType] {
			return nil, &RoutingFailure{Stream: row.Stream, Reason: "unknown event_type", Field: "event_type", Value: row.EventType}
		}
		if row.TenantID == "" {
			return nil, &RoutingFailure{Stream: row.Stream, Reason: "missing tenant_id", Field: "tenant_id", Value: ""}
		}
		return map[string]string{
			"event_type": row.EventType,
			"tenant_id":  row.TenantID,
		}, nil
	case StreamGeneration:
		if !generationTiers[row.Tier] {
			return nil, &RoutingFailure{Stream: row.Stream, Reason: "unknown tier", Field: "tier", Value: row.Tier}
		}
		if !generationStages[row.Stage] {
			return nil, &RoutingFailure{Stream: row.Stream, Reason: "unknown stage", Field: "stage", Value: row.Stage}
		}
		if !generationResourceClasses[row.ResourceClass] {
			return nil, &RoutingFailure{Stream: row.Stream, Reason: "unknown resource_class", Field: "resource_class", Value: row.ResourceClass}
		}
		if row.TenantLane == "" {
			return nil, &RoutingFailure{Stream: row.Stream, Reason: "missing tenant_lane", Field: "tenant_lane", Value: ""}
		}
		return map[string]string{
			"tier":           row.Tier,
			"stage":          row.Stage,
			"resource_class": row.ResourceClass,
			"tenant_lane":    row.TenantLane,
		}, nil
	default:
		return nil, &RoutingFailure{Stream: row.Stream, Reason: "unknown stream", Field: "stream", Value: row.Stream}
	}
}

// Closed enums for routing-policy validation. Sourced from the canonical
// domain + events packages so adding a new value in one place fails the
// policy until it's also enumerated here — the relay is then the explicit
// gate on stream evolution rather than a silent passthrough.
var (
	mediaEventTypes = map[string]bool{
		"media.v1.process":   true,
		"media.v1.completed": true,
		"media.v1.failed":    true,
		"media.v1.delete":    true,
	}

	generationTiers = map[string]bool{
		string(domaingen.TierFree): true,
		string(domaingen.TierPaid): true,
	}

	generationStages = map[string]bool{
		string(domaingen.StageInputModeration):       true,
		string(domaingen.StageCostReserve):           true,
		string(domaingen.StagePromptPrepare):         true,
		string(domaingen.StageProviderSubmit):        true,
		string(domaingen.StageProviderWait):          true,
		string(domaingen.StageOutputModeration):      true,
		string(domaingen.StageDisclosurePostprocess): true,
		string(domaingen.StagePublish):               true,
	}

	generationResourceClasses = map[string]bool{
		string(domaingen.ResourceFast):         true,
		string(domaingen.ResourceProvider):     true,
		string(domaingen.ResourcePoll):         true,
		string(domaingen.ResourceImageProcess): true,
	}
)
