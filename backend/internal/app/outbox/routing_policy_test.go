package outbox

import (
	"errors"
	"testing"

	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// allDomainStages mirrors the closed set of Stage const values declared in
// domain/generation/types.go. Keeping the list explicit makes policy drift
// visible: the guard below fails when the routing policy carries stale stages
// or when this test list is not updated with a new domain stage. The policy is
// the only gate against silent SNS-filter-policy drops; under-covering it here
// puts every misrouted stage into a queued-but-unconsumed black hole.
var allDomainStages = []domaingen.Stage{
	domaingen.StageInputModeration,
	domaingen.StageCostReserve,
	domaingen.StagePromptPrepare,
	domaingen.StageProviderSubmit,
	domaingen.StageProviderWait,
	domaingen.StageOutputModeration,
	domaingen.StageDisclosurePostprocess,
	domaingen.StagePublish,
}

var allDomainTiers = []domaingen.Tier{
	domaingen.TierFree,
	domaingen.TierPaid,
}

var allDomainResourceClasses = []domaingen.ResourceClass{
	domaingen.ResourceFast,
	domaingen.ResourceProvider,
	domaingen.ResourcePoll,
	domaingen.ResourceImageProcess,
}

var allMediaEventTypes = []string{
	"media.v1.process",
	"media.v1.completed",
	"media.v1.failed",
	"media.v1.delete",
}

// TestDefaultPolicy_HappyPaths covers one canonical row per stream so a
// regression that breaks attribute derivation surfaces in a single failure.
func TestDefaultPolicy_HappyPaths(t *testing.T) {
	cases := []struct {
		name string
		row  PendingRow
		want map[string]string
	}{
		{
			name: "media stream derives event_type + tenant_id",
			row:  PendingRow{Stream: StreamMedia, EventType: "media.v1.process", TenantID: "tenant-a"},
			want: map[string]string{"event_type": "media.v1.process", "tenant_id": "tenant-a"},
		},
		{
			name: "media cleanup stream shares the media attribute set",
			row:  PendingRow{Stream: StreamMediaCleanup, EventType: "media.v1.delete", TenantID: "tenant-b"},
			want: map[string]string{"event_type": "media.v1.delete", "tenant_id": "tenant-b"},
		},
		{
			name: "generation stream derives tier + stage + resource_class",
			row: PendingRow{
				Stream:        StreamGeneration,
				Tier:          string(domaingen.TierPaid),
				Stage:         string(domaingen.StageProviderSubmit),
				ResourceClass: string(domaingen.ResourceProvider),
				TenantLane:    "lane-test",
			},
			want: map[string]string{
				"tier":           "PAID",
				"stage":          "PROVIDER_SUBMIT",
				"resource_class": "PROVIDER",
				"tenant_lane":    "lane-test",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DefaultPolicy{}.AttributesFor(tc.row)
			if err != nil {
				t.Fatalf("AttributesFor returned err=%v", err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("attribute count mismatch: got=%v want=%v", got, tc.want)
			}
			for k, v := range tc.want {
				if got[k] != v {
					t.Errorf("attr[%q]=%q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestDefaultPolicy_EnumeratesEveryMediaEventType(t *testing.T) {
	for _, stream := range []string{StreamMedia, StreamMediaCleanup} {
		for _, eventType := range allMediaEventTypes {
			row := PendingRow{Stream: stream, EventType: eventType, TenantID: "tenant-a"}
			got, err := DefaultPolicy{}.AttributesFor(row)
			if err != nil {
				t.Fatalf("AttributesFor(%s, %s) returned err=%v", stream, eventType, err)
			}
			if got["event_type"] != eventType {
				t.Errorf("event_type attr = %q, want %q", got["event_type"], eventType)
			}
			if got["tenant_id"] != "tenant-a" {
				t.Errorf("tenant_id attr = %q, want tenant-a", got["tenant_id"])
			}
		}
	}
	if len(mediaEventTypes) != len(allMediaEventTypes) {
		t.Errorf("mediaEventTypes has %d entries, test list has %d", len(mediaEventTypes), len(allMediaEventTypes))
	}
}

// TestDefaultPolicy_RejectsInvalidRows asserts every field the policy guards
// surfaces a *RoutingFailure (wrapped ErrRoutingPolicyFailed) rather than
// returning a partial attribute set. RoutingFailure.Field is what the relay
// puts on the DLQ row, so it doubles as the ops contract.
func TestDefaultPolicy_RejectsInvalidRows(t *testing.T) {
	cases := []struct {
		name       string
		row        PendingRow
		wantStream string
		wantReason string
		wantField  string
		wantValue  string
	}{
		{
			name:       "unknown stream",
			row:        PendingRow{Stream: "TELEMETRY", EventType: "media.v1.process", TenantID: "tenant"},
			wantStream: "TELEMETRY",
			wantReason: "unknown stream",
			wantField:  "stream",
			wantValue:  "TELEMETRY",
		},
		{
			name:       "media: unknown event_type",
			row:        PendingRow{Stream: StreamMedia, EventType: "media.v1.unknown", TenantID: "tenant"},
			wantStream: StreamMedia,
			wantReason: "unknown event_type",
			wantField:  "event_type",
			wantValue:  "media.v1.unknown",
		},
		{
			name:       "media_cleanup: unknown event_type",
			row:        PendingRow{Stream: StreamMediaCleanup, EventType: "cleanup.v1.delete", TenantID: "tenant"},
			wantStream: StreamMediaCleanup,
			wantReason: "unknown event_type",
			wantField:  "event_type",
			wantValue:  "cleanup.v1.delete",
		},
		{
			name:       "media: missing tenant_id",
			row:        PendingRow{Stream: StreamMedia, EventType: "media.v1.process"},
			wantStream: StreamMedia,
			wantReason: "missing tenant_id",
			wantField:  "tenant_id",
			wantValue:  "",
		},
		{
			name:       "media_cleanup: missing tenant_id",
			row:        PendingRow{Stream: StreamMediaCleanup, EventType: "media.v1.delete"},
			wantStream: StreamMediaCleanup,
			wantReason: "missing tenant_id",
			wantField:  "tenant_id",
			wantValue:  "",
		},
		{
			name: "generation: unknown tier",
			row: PendingRow{
				Stream:        StreamGeneration,
				Tier:          "ENTERPRISE",
				Stage:         string(domaingen.StageProviderSubmit),
				ResourceClass: string(domaingen.ResourceProvider),
			},
			wantStream: StreamGeneration,
			wantReason: "unknown tier",
			wantField:  "tier",
			wantValue:  "ENTERPRISE",
		},
		{
			name: "generation: unknown stage",
			row: PendingRow{
				Stream:        StreamGeneration,
				Tier:          string(domaingen.TierFree),
				Stage:         "AUDIT",
				ResourceClass: string(domaingen.ResourceFast),
			},
			wantStream: StreamGeneration,
			wantReason: "unknown stage",
			wantField:  "stage",
			wantValue:  "AUDIT",
		},
		{
			name: "generation: unknown resource_class",
			row: PendingRow{
				Stream:        StreamGeneration,
				Tier:          string(domaingen.TierFree),
				Stage:         string(domaingen.StageProviderSubmit),
				ResourceClass: "GPU_H100",
			},
			wantStream: StreamGeneration,
			wantReason: "unknown resource_class",
			wantField:  "resource_class",
			wantValue:  "GPU_H100",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			attrs, err := DefaultPolicy{}.AttributesFor(tc.row)
			if attrs != nil {
				t.Fatalf("expected nil attrs on rejection, got %v", attrs)
			}
			if !errors.Is(err, ErrRoutingPolicyFailed) {
				t.Fatalf("error does not unwrap to ErrRoutingPolicyFailed: %v", err)
			}
			var rf *RoutingFailure
			if !errors.As(err, &rf) {
				t.Fatalf("expected *RoutingFailure, got %T", err)
			}
			if rf.Stream != tc.wantStream {
				t.Errorf("RoutingFailure.Stream=%q, want %q", rf.Stream, tc.wantStream)
			}
			if rf.Reason != tc.wantReason {
				t.Errorf("RoutingFailure.Reason=%q, want %q", rf.Reason, tc.wantReason)
			}
			if rf.Field != tc.wantField {
				t.Errorf("RoutingFailure.Field=%q, want %q", rf.Field, tc.wantField)
			}
			if rf.Value != tc.wantValue {
				t.Errorf("RoutingFailure.Value=%q, want %q", rf.Value, tc.wantValue)
			}
		})
	}
}

// TestDefaultPolicy_EnumeratesEveryDomainStage is the load-bearing guard.
// If domain/generation adds a Stage value and the developer forgets to
// extend generationStages, every JobRow at that stage publishes with a
// missing or wrong attribute and gets silently dropped behind the SNS
// filter policy. Iterating over the closed domain enum here turns that
// silent failure into a red test.
func TestDefaultPolicy_EnumeratesEveryDomainStage(t *testing.T) {
	for _, stage := range allDomainStages {
		row := PendingRow{
			Stream:        StreamGeneration,
			Tier:          string(domaingen.TierFree),
			Stage:         string(stage),
			ResourceClass: string(domaingen.ResourceFast),
			TenantLane:    "lane-test",
		}
		if _, err := (DefaultPolicy{}).AttributesFor(row); err != nil {
			t.Errorf("policy rejects domain stage %q — generationStages is out of sync with domain/generation: %v", stage, err)
		}
	}
	if len(generationStages) != len(allDomainStages) {
		t.Errorf("generationStages has %d entries, domain has %d — the policy may carry stale stages",
			len(generationStages), len(allDomainStages))
	}
}

// TestDefaultPolicy_EnumeratesEveryDomainTier mirrors the Stage guard for
// the tier enum, which drives the per-tier SNS fanout filter.
func TestDefaultPolicy_EnumeratesEveryDomainTier(t *testing.T) {
	for _, tier := range allDomainTiers {
		row := PendingRow{
			Stream:        StreamGeneration,
			Tier:          string(tier),
			Stage:         string(domaingen.StageProviderSubmit),
			ResourceClass: string(domaingen.ResourceProvider),
			TenantLane:    "lane-test",
		}
		if _, err := (DefaultPolicy{}).AttributesFor(row); err != nil {
			t.Errorf("policy rejects domain tier %q — generationTiers is out of sync with domain/generation: %v", tier, err)
		}
	}
	if len(generationTiers) != len(allDomainTiers) {
		t.Errorf("generationTiers has %d entries, domain has %d", len(generationTiers), len(allDomainTiers))
	}
}

// TestDefaultPolicy_EnumeratesEveryDomainResourceClass guards the
// resource_class side of the per-tier × resource-class queue fanout.
func TestDefaultPolicy_EnumeratesEveryDomainResourceClass(t *testing.T) {
	for _, rc := range allDomainResourceClasses {
		row := PendingRow{
			Stream:        StreamGeneration,
			Tier:          string(domaingen.TierFree),
			Stage:         string(domaingen.StageProviderSubmit),
			ResourceClass: string(rc),
			TenantLane:    "lane-test",
		}
		if _, err := (DefaultPolicy{}).AttributesFor(row); err != nil {
			t.Errorf("policy rejects domain resource class %q — generationResourceClasses is out of sync with domain/generation: %v", rc, err)
		}
	}
	if len(generationResourceClasses) != len(allDomainResourceClasses) {
		t.Errorf("generationResourceClasses has %d entries, domain has %d", len(generationResourceClasses), len(allDomainResourceClasses))
	}
}

// TestDefaultPolicy_EnumeratesEveryStream guards the stream switch in
// AttributesFor against an AllStreams entry that lacks a case branch — the
// kind of typo that would route every row on the new stream to DLQ.
func TestDefaultPolicy_EnumeratesEveryStream(t *testing.T) {
	for _, stream := range AllStreams {
		row := minimalRowFor(stream)
		if _, err := (DefaultPolicy{}).AttributesFor(row); err != nil {
			t.Errorf("policy rejects declared stream %q — AttributesFor missing a case branch: %v", stream, err)
		}
	}
}

// minimalRowFor returns the smallest PendingRow that should pass the policy
// for stream. Used by the AllStreams guard so adding a stream forces the
// developer to either extend this helper or fail the guard test loudly.
func minimalRowFor(stream string) PendingRow {
	switch stream {
	case StreamMedia, StreamMediaCleanup:
		return PendingRow{Stream: stream, EventType: "media.v1.process", TenantID: "tenant"}
	case StreamGeneration:
		return PendingRow{
			Stream:        stream,
			Tier:          string(domaingen.TierFree),
			Stage:         string(domaingen.StageProviderSubmit),
			ResourceClass: string(domaingen.ResourceProvider),
			TenantLane:    "lane-test",
		}
	default:
		return PendingRow{Stream: stream}
	}
}
