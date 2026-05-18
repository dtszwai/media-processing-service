package ops

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
)

func TestDecodeJobSummaryReadsGenRowAttributes(t *testing.T) {
	created := time.Date(2026, 5, 17, 2, 31, 51, 0, time.UTC)
	completed := created.Add(2 * time.Second)
	av, err := attributevalue.MarshalMap(map[string]any{
		"PK":            "JOB#job-1",
		"SK":            "JOB",
		"item_type":     "GEN",
		"id":            "job-1",
		"tenant_id":     "tenant-1",
		"media_id":      "media-1",
		"status":        "FAILED",
		"current_stage": "TERMINAL",
		"output_type":   "IMAGE",
		"tier":          "PAID",
		"model":         "image-1",
		"attempts":      3,
		"error_code":    "PROVIDER_FAILED",
		"created_at":    created,
		"updated_at":    completed,
		"completed_at":  completed,
	})
	if err != nil {
		t.Fatalf("marshal job row: %v", err)
	}

	got, ok := decodeJobSummary(av)
	if !ok {
		t.Fatal("decodeJobSummary returned ok=false")
	}
	if got.JobID != "job-1" || got.TenantID != "tenant-1" || got.ErrorCode != "PROVIDER_FAILED" {
		t.Fatalf("summary identifiers/error = %+v", got)
	}
	if got.Status != "FAILED" || got.CurrentStage != "TERMINAL" || got.OutputType != "IMAGE" || got.Tier != "PAID" {
		t.Fatalf("summary state = %+v", got)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completed) {
		t.Fatalf("CompletedAt = %v, want %s", got.CompletedAt, completed)
	}
}

func TestAttemptSpanProviderUnavailableIsWorkerPrecheck(t *testing.T) {
	row := map[string]any{
		"stage":          "INPUT_MODERATION",
		"stage_version":  float64(1),
		"attempt_no":     float64(1),
		"result":         "TERMINAL_FAILURE",
		"error_code":     "PROVIDER_UNAVAILABLE",
		"error_message":  "codex not registered on this worker",
		"resource_class": "FAST",
		"created_at":     "2026-05-17T02:31:53Z",
	}

	span := attemptSpan(row)

	if span.Stage != workerPrecheckStage {
		t.Fatalf("Stage = %q, want %q", span.Stage, workerPrecheckStage)
	}
	if span.Label != "provider resolution" {
		t.Fatalf("Label = %q, want provider resolution", span.Label)
	}
	if span.Attributes["recorded_stage"] != "INPUT_MODERATION" {
		t.Fatalf("recorded_stage = %q, want INPUT_MODERATION", span.Attributes["recorded_stage"])
	}
}

func TestAttemptSpanIncludesTraceAttributes(t *testing.T) {
	span := attemptSpan(map[string]any{
		"stage":          "INPUT_MODERATION",
		"stage_version":  float64(1),
		"attempt_no":     float64(1),
		"result":         "SUCCESS",
		"resource_class": "FAST",
		"created_at":     "2026-05-17T02:31:53Z",
		"traceparent":    "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		"trace_id":       "4bf92f3577b34da6a3ce929d0e0e4736",
	})

	if span.Attributes["traceparent"] == "" {
		t.Fatalf("traceparent attribute missing")
	}
	if span.Attributes["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id = %q, want propagated trace id", span.Attributes["trace_id"])
	}
}

func TestOutputSpanQueuedIsPendingRecord(t *testing.T) {
	span := outputSpan(map[string]any{
		"output_id":  "out-1",
		"type":       "IMAGE",
		"status":     "QUEUED",
		"created_at": "2026-05-17T02:31:51Z",
	})

	if span.Label != "output record · QUEUED" {
		t.Fatalf("Label = %q, want output record label", span.Label)
	}
	if span.Status != "PENDING" {
		t.Fatalf("Status = %q, want PENDING", span.Status)
	}
	if span.Stage != "PUBLISH" {
		t.Fatalf("Stage = %q, want PUBLISH", span.Stage)
	}
}

func TestOutputSpanTerminalStatusesAreNotPending(t *testing.T) {
	for _, status := range []string{"FAILED", "CANCELLED"} {
		span := outputSpan(map[string]any{
			"output_id":    "out-" + status,
			"type":         "IMAGE",
			"status":       status,
			"created_at":   "2026-05-17T02:31:51Z",
			"completed_at": "2026-05-17T02:31:53Z",
		})

		if span.Status != "TERMINAL_FAIL" {
			t.Fatalf("status %s mapped to %q, want TERMINAL_FAIL", status, span.Status)
		}
		if got, want := span.StartAt, time.Date(2026, 5, 17, 2, 31, 53, 0, time.UTC); !got.Equal(want) {
			t.Fatalf("status %s StartAt = %s, want %s", status, got, want)
		}
	}
}

func TestOutputSpanCompleteRendersAtCompletionNotSlotCreation(t *testing.T) {
	span := outputSpan(map[string]any{
		"output_id":    "out-complete",
		"type":         "IMAGE",
		"status":       "COMPLETE",
		"created_at":   "2026-05-17T02:31:51Z",
		"completed_at": "2026-05-17T02:31:55Z",
	})

	want := time.Date(2026, 5, 17, 2, 31, 55, 0, time.UTC)
	if !span.StartAt.Equal(want) || !span.EndAt.Equal(want) {
		t.Fatalf("output span window = %s → %s, want completion point %s", span.StartAt, span.EndAt, want)
	}
}

func TestCloseStageEndsUsesTerminalAuditBeforeNow(t *testing.T) {
	start := time.Date(2026, 5, 17, 2, 31, 53, 0, time.UTC)
	now := start.Add(10 * time.Minute)
	spans := []TraceSpan{
		{ID: "stage:WORKER_PRECHECK", Kind: "STAGE", Stage: workerPrecheckStage, StartAt: start, EndAt: start},
		{ID: "terminal-audit", Kind: "TERMINAL_AUDIT", StartAt: start.Add(200 * time.Millisecond), EndAt: start.Add(200 * time.Millisecond)},
	}

	closeStageEnds(spans, now)

	if got := spans[0].EndAt; !got.Equal(start.Add(200 * time.Millisecond)) {
		t.Fatalf("EndAt = %s, want terminal audit timestamp", got)
	}
	if spans[0].Attributes["end_synthesized"] != "" {
		t.Fatalf("end_synthesized = %q, want empty (terminal audit is observed)", spans[0].Attributes["end_synthesized"])
	}
}

// PROMPT_PREPARE has no observable child events — only its own ATTEMPT row.
// The 2-minute gap before PROVIDER_SUBMIT picks up the next message is
// queue/handoff time, not stage work. We still stretch to the next stage
// for visual continuity but tag the synthesis so the UI can render it as
// a projection rather than as observed work.
func TestCloseStageEndsMarksSynthesizedEndWhenStretching(t *testing.T) {
	start := time.Date(2026, 5, 17, 9, 21, 19, 0, time.UTC)
	next := start.Add(2*time.Minute + time.Second)
	spans := []TraceSpan{
		{ID: "stage:PROMPT_PREPARE", Kind: "STAGE", Stage: "PROMPT_PREPARE", StartAt: start, EndAt: start},
		{ID: "stage:PROVIDER_SUBMIT", Kind: "STAGE", Stage: "PROVIDER_SUBMIT", StartAt: next, EndAt: next},
	}

	closeStageEnds(spans, next.Add(time.Second))

	if got := spans[0].EndAt; !got.Equal(next) {
		t.Fatalf("PROMPT_PREPARE.EndAt = %s, want next stage start %s", got, next)
	}
	if got := spans[0].Attributes["end_synthesized"]; got != "next_stage_start" {
		t.Fatalf("end_synthesized = %q, want next_stage_start", got)
	}
}

// A stage with a PROVIDER_REQUEST child should bound its EndAt by the
// provider's completed_at, not by the FSM's next-stage transition.
func TestCloseStageEndsPrefersProviderRequestEndOverNextStageStart(t *testing.T) {
	start := time.Date(2026, 5, 17, 9, 23, 20, 0, time.UTC)
	providerEnd := start.Add(5 * time.Second)
	next := start.Add(2 * time.Minute)
	spans := []TraceSpan{
		{ID: "stage:PROVIDER_SUBMIT", Kind: "STAGE", Stage: "PROVIDER_SUBMIT", StartAt: start, EndAt: start},
		{ID: "provider:req-1", Kind: "PROVIDER_REQUEST", Status: "OK", Stage: "PROVIDER_SUBMIT", StartAt: start, EndAt: providerEnd, Attributes: map[string]string{"status": "SUCCEEDED"}},
		{ID: "stage:OUTPUT_MODERATION", Kind: "STAGE", Stage: "OUTPUT_MODERATION", StartAt: next, EndAt: next},
	}

	closeStageEnds(spans, next.Add(time.Second))

	if got := spans[0].EndAt; !got.Equal(providerEnd) {
		t.Fatalf("PROVIDER_SUBMIT.EndAt = %s, want providerEnd %s (observed > next stage start should win)", got, providerEnd)
	}
	if spans[0].Attributes["end_synthesized"] != "" {
		t.Fatalf("end_synthesized = %q, want empty (observed end exists)", spans[0].Attributes["end_synthesized"])
	}
}

// PROMPT_PREPARE has no observable children, so it falls back to the
// next-stage-start projection. But the next stage's STAGE row is
// derived from its ATTEMPT, which fires when AdvanceStageAndEnqueue runs
// at the *end* of the next stage's work — so the next stage's initial
// StartAt is its work-finish time, not its work-start time. The fix
// rewrites every stage's StartAt against observable child signals
// *before* using them as anchors, so the synthesized PROMPT_PREPARE bar
// stops at the next stage's real begin (the PROVIDER_REQUEST.StartAt),
// not its eventual finish.
func TestCloseStageEndsSynthesizedEndTracksObservedNextStageStart(t *testing.T) {
	promptStart := time.Date(2026, 5, 17, 20, 0, 27, 0, time.UTC)
	providerWorkStart := promptStart.Add(2 * time.Second)
	providerEnd := promptStart.Add(2*time.Minute + 28*time.Second)
	spans := []TraceSpan{
		{ID: "stage:PROMPT_PREPARE", Kind: "STAGE", Stage: "PROMPT_PREPARE", StartAt: promptStart, EndAt: promptStart, Attributes: map[string]string{}},
		// PROVIDER_SUBMIT STAGE row carries the attempt timestamp because
		// deriveStageSpans seeds it from the ATTEMPT row. The real begin
		// of provider work is on the PROVIDER_REQUEST child below.
		{ID: "stage:PROVIDER_SUBMIT", Kind: "STAGE", Stage: "PROVIDER_SUBMIT", StartAt: providerEnd, EndAt: providerEnd, Attributes: map[string]string{}},
		{ID: "provider:req-1", Kind: "PROVIDER_REQUEST", Status: "OK", Stage: "PROVIDER_SUBMIT", StartAt: providerWorkStart, EndAt: providerEnd, Attributes: map[string]string{"status": "SUCCEEDED"}},
	}

	closeStageEnds(spans, providerEnd.Add(time.Second))

	if got := spans[0].EndAt; !got.Equal(providerWorkStart) {
		t.Fatalf("PROMPT_PREPARE.EndAt = %s, want observed PROVIDER_SUBMIT start %s (attempt time %s would mean the bar spans the next stage's entire wall-clock)",
			got, providerWorkStart, providerEnd)
	}
	if got := spans[0].Attributes["end_synthesized"]; got != "next_stage_start" {
		t.Fatalf("end_synthesized = %q, want next_stage_start", got)
	}
}

func TestCloseStageEndsPinsTransientTailToAttemptTime(t *testing.T) {
	start := time.Date(2026, 5, 17, 9, 39, 6, 0, time.UTC)
	now := start.Add(10 * time.Minute)
	spans := []TraceSpan{
		{ID: "stage:PROVIDER_SUBMIT", Kind: "STAGE", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", StartAt: start, EndAt: start, Attributes: map[string]string{}},
		{ID: "attempt:PROVIDER_SUBMIT:v1:a1", Kind: "ATTEMPT", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", AttemptNo: 1, StartAt: start, EndAt: start, ErrorCode: "NOTEBOOKLM_RPC_FAILURE"},
	}

	closeStageEnds(spans, now)

	if got := spans[0].EndAt; !got.Equal(start) {
		t.Fatalf("PROVIDER_SUBMIT.EndAt = %s, want transient attempt timestamp %s", got, start)
	}
	if got := spans[0].DurationMS; got != 0 {
		t.Fatalf("DurationMS = %d, want 0 for point-in-time transient outcome", got)
	}
	if got := spans[0].Attributes["end_synthesized"]; got != "" {
		t.Fatalf("end_synthesized = %q, want empty", got)
	}
	if got := spans[0].Attributes[retryStateAttr]; got != retryStateAwaiting {
		t.Fatalf("retry_state = %q, want %q", got, retryStateAwaiting)
	}
}

func TestCloseStageEndsMarksTransientTailStuckAfterRetryWindow(t *testing.T) {
	start := time.Date(2026, 5, 17, 9, 39, 6, 0, time.UTC)
	spans := []TraceSpan{
		{ID: "stage:PROVIDER_SUBMIT", Kind: "STAGE", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", StartAt: start, EndAt: start, Attributes: map[string]string{}},
		{ID: "attempt:PROVIDER_SUBMIT:v1:a1", Kind: "ATTEMPT", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", AttemptNo: 1, StartAt: start, EndAt: start},
	}

	closeStageEnds(spans, start.Add(retryStuckAfter+time.Second))

	if got := spans[0].EndAt; !got.Equal(start) {
		t.Fatalf("PROVIDER_SUBMIT.EndAt = %s, want stable transient timestamp %s", got, start)
	}
	if got := spans[0].Attributes[retryStateAttr]; got != retryStateStuck {
		t.Fatalf("retry_state = %q, want %q", got, retryStateStuck)
	}
}

func TestCloseStageEndsUsesTerminalProviderRequestForTransientTail(t *testing.T) {
	providerStart := time.Date(2026, 5, 17, 9, 39, 0, 0, time.UTC)
	providerEnd := providerStart.Add(2 * time.Second)
	attemptAt := providerStart.Add(3 * time.Second)
	spans := []TraceSpan{
		{ID: "stage:PROVIDER_SUBMIT", Kind: "STAGE", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", StartAt: attemptAt, EndAt: attemptAt, Attributes: map[string]string{}},
		{ID: "provider:req-1", Kind: "PROVIDER_REQUEST", Status: "TERMINAL_FAIL", Stage: "PROVIDER_SUBMIT", StartAt: providerStart, EndAt: providerEnd, Attributes: map[string]string{"status": "FAILED"}},
		{ID: "attempt:PROVIDER_SUBMIT:v1:a1", Kind: "ATTEMPT", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", AttemptNo: 1, StartAt: attemptAt, EndAt: attemptAt},
	}

	closeStageEnds(spans, attemptAt.Add(time.Minute))

	if got := spans[0].StartAt; !got.Equal(providerStart) {
		t.Fatalf("PROVIDER_SUBMIT.StartAt = %s, want provider request start %s", got, providerStart)
	}
	if got := spans[0].EndAt; !got.Equal(providerEnd) {
		t.Fatalf("PROVIDER_SUBMIT.EndAt = %s, want terminal provider request end %s", got, providerEnd)
	}
	if got := spans[0].DurationMS; got != providerEnd.Sub(providerStart).Milliseconds() {
		t.Fatalf("DurationMS = %d, want provider request duration", got)
	}
	if got := spans[0].Attributes[retryStateAttr]; got != retryStateAwaiting {
		t.Fatalf("retry_state = %q, want %q", got, retryStateAwaiting)
	}
}

// Backstop: if the data lands with EndAt < StartAt for any reason (out-of-
// order FSM writes, clock skew), the visible duration must clamp to 0
// instead of rendering as a negative bar in the waterfall.
func TestCloseStageEndsClampsNegativeDuration(t *testing.T) {
	start := time.Date(2026, 5, 17, 9, 23, 25, 0, time.UTC)
	spans := []TraceSpan{
		{ID: "stage:PUBLISH", Kind: "STAGE", Stage: "PUBLISH", StartAt: start, EndAt: start.Add(-1530 * time.Millisecond)},
	}

	closeStageEnds(spans, start.Add(time.Second))

	if spans[0].DurationMS < 0 {
		t.Fatalf("DurationMS = %d, want non-negative", spans[0].DurationMS)
	}
	if spans[0].EndAt.Before(spans[0].StartAt) {
		t.Fatalf("EndAt %s is before StartAt %s after clamping", spans[0].EndAt, spans[0].StartAt)
	}
}

// A fresh non-terminal PROVIDER_REQUEST landing after the failed attempt
// flips the stage into the "retrying" state — that's the only data signal
// that proves a worker has actually picked the SQS message back up and is
// calling the provider again before the next ATTEMPT row is written.
func TestAnnotateRetryStateMarksRetryingOnFreshProviderRequest(t *testing.T) {
	attemptAt := time.Date(2026, 5, 17, 9, 39, 6, 0, time.UTC)
	retryStart := attemptAt.Add(2 * time.Second)
	spans := []TraceSpan{
		{ID: "stage:PROVIDER_SUBMIT", Kind: "STAGE", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", StartAt: attemptAt, EndAt: attemptAt, Attributes: map[string]string{}},
		{ID: "attempt:PROVIDER_SUBMIT:v1:a1", Kind: "ATTEMPT", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", AttemptNo: 1, StartAt: attemptAt, EndAt: attemptAt},
		{ID: "provider:req-2", Kind: "PROVIDER_REQUEST", Status: "PENDING", Stage: "PROVIDER_SUBMIT", StartAt: retryStart, EndAt: retryStart, Attributes: map[string]string{"status": "PENDING"}},
	}

	closeStageEnds(spans, retryStart.Add(time.Second))

	if got := spans[0].Attributes[retryStateAttr]; got != retryStateRetrying {
		t.Fatalf("retry_state = %q, want %q (fresh provider request should signal retrying)", got, retryStateRetrying)
	}
}

// A PENDING OUTPUT row whose insertion timestamp lands after the failed
// attempt must NOT flip the stage into "retrying". OUTPUT rows are
// downstream artefacts; their timestamps move independently of any retry
// activity, so using them as a retry signal was the bug that made
// "retrying" stick on jobs that were actually awaiting redelivery.
func TestAnnotateRetryStateIgnoresPendingOutputAfterAttempt(t *testing.T) {
	attemptAt := time.Date(2026, 5, 17, 9, 39, 6, 0, time.UTC)
	outputAt := attemptAt.Add(30 * time.Second)
	spans := []TraceSpan{
		{ID: "stage:PROVIDER_SUBMIT", Kind: "STAGE", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", StartAt: attemptAt, EndAt: attemptAt, Attributes: map[string]string{}},
		{ID: "attempt:PROVIDER_SUBMIT:v1:a1", Kind: "ATTEMPT", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", AttemptNo: 1, StartAt: attemptAt, EndAt: attemptAt},
		{ID: "output:out-1", Kind: "OUTPUT", Status: "PENDING", Stage: "PROVIDER_SUBMIT", StartAt: outputAt, EndAt: outputAt, Attributes: map[string]string{"status": "QUEUED"}},
	}

	closeStageEnds(spans, outputAt.Add(time.Second))

	if got := spans[0].Attributes[retryStateAttr]; got != retryStateAwaiting {
		t.Fatalf("retry_state = %q, want %q (PENDING OUTPUT is not a retry signal)", got, retryStateAwaiting)
	}
}

// A non-terminal PROVIDER_REQUEST that landed in the same millisecond as
// the failing attempt is almost certainly the request the attempt itself
// made — not a retry. The 500ms margin in hasFreshProviderRetryAfter
// exists to keep that case out of the "retrying" bucket.
func TestAnnotateRetryStateRejectsSameCycleProviderRequest(t *testing.T) {
	attemptAt := time.Date(2026, 5, 17, 9, 39, 6, 0, time.UTC)
	sameCycle := attemptAt.Add(50 * time.Millisecond)
	spans := []TraceSpan{
		{ID: "stage:PROVIDER_SUBMIT", Kind: "STAGE", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", StartAt: attemptAt, EndAt: attemptAt, Attributes: map[string]string{}},
		{ID: "attempt:PROVIDER_SUBMIT:v1:a1", Kind: "ATTEMPT", Status: "TRANSIENT_FAIL", Stage: "PROVIDER_SUBMIT", AttemptNo: 1, StartAt: attemptAt, EndAt: attemptAt},
		{ID: "provider:req-1", Kind: "PROVIDER_REQUEST", Status: "PENDING", Stage: "PROVIDER_SUBMIT", StartAt: sameCycle, EndAt: sameCycle, Attributes: map[string]string{"status": "PENDING"}},
	}

	closeStageEnds(spans, attemptAt.Add(time.Minute))

	if got := spans[0].Attributes[retryStateAttr]; got != retryStateAwaiting {
		t.Fatalf("retry_state = %q, want %q (same-cycle provider request inside the margin must not flip to retrying)", got, retryStateAwaiting)
	}
}

func TestDisclosureGateClassification(t *testing.T) {
	if isDisclosureGateAudit(&GateDecisionView{Decision: "FAIL", ErrorCode: "PROVIDER_UNAVAILABLE"}) {
		t.Fatalf("PROVIDER_UNAVAILABLE should render as terminal audit, not disclosure gate")
	}
	if !isDisclosureGateAudit(&GateDecisionView{Decision: "FAIL", ErrorCode: "AI_DISCLOSURE_MISSING"}) {
		t.Fatalf("AI_DISCLOSURE_MISSING should render as disclosure gate")
	}
}

func TestDisclosureGateSpanParentsToPostprocessStage(t *testing.T) {
	span := disclosureGateSpan(&GateDecisionView{
		Decision:  "PASS",
		DecidedAt: time.Date(2026, 5, 17, 2, 31, 55, 0, time.UTC),
	}, "AUDIT#GATE#gen_test", "v1")

	if span.Kind != "GATE_AUDIT" {
		t.Fatalf("Kind = %q, want GATE_AUDIT", span.Kind)
	}
	if span.Stage != "DISCLOSURE_POSTPROCESS" {
		t.Fatalf("Stage = %q, want DISCLOSURE_POSTPROCESS", span.Stage)
	}
	if span.ParentID != "stage:DISCLOSURE_POSTPROCESS" {
		t.Fatalf("ParentID = %q, want stage:DISCLOSURE_POSTPROCESS", span.ParentID)
	}
}

// While the worker is mid-PROVIDER_SUBMIT, no STAGE_ATTEMPT row for that
// stage exists yet — AdvanceStageAndEnqueue writes it at end-of-stage. Without
// a synthesized stage span, PROMPT_PREPARE was the visible tail and its bar
// grew to `now`. runningStageSpan caps that by materializing PROVIDER_SUBMIT
// anchored at the earliest observable child (the PROVIDER_REQUEST start).
func TestRunningStageSpanCapsPreviousStageAtCurrentStageStart(t *testing.T) {
	promptAttemptAt := time.Date(2026, 5, 17, 20, 51, 50, 0, time.UTC)
	providerStartedAt := promptAttemptAt.Add(2 * time.Second)
	now := promptAttemptAt.Add(time.Minute + 8*time.Second)
	summary := JobSummary{
		Status:       "RUNNING",
		CurrentStage: "PROVIDER_SUBMIT",
		UpdatedAt:    promptAttemptAt,
	}
	spans := []TraceSpan{
		{ID: "attempt:PROMPT_PREPARE:v1:a1", Kind: "ATTEMPT", Stage: "PROMPT_PREPARE", Status: "OK",
			AttemptNo: 1, StartAt: promptAttemptAt, EndAt: promptAttemptAt},
		{ID: "provider:req-1", Kind: "PROVIDER_REQUEST", Stage: "PROVIDER_SUBMIT", Status: "PENDING",
			StartAt: providerStartedAt, Attributes: map[string]string{"status": "PENDING"}},
	}
	spans = append(spans, deriveStageSpans(spans)...)
	extra, ok := runningStageSpan(summary, spans, now)
	if !ok {
		t.Fatal("runningStageSpan ok=false; want synthetic stage for in-flight current_stage")
	}
	spans = append(spans, extra)
	closeStageEnds(spans, now)

	var prompt, submit *TraceSpan
	for i := range spans {
		if spans[i].Kind != "STAGE" {
			continue
		}
		switch spans[i].Stage {
		case "PROMPT_PREPARE":
			prompt = &spans[i]
		case "PROVIDER_SUBMIT":
			submit = &spans[i]
		}
	}
	if prompt == nil || submit == nil {
		t.Fatalf("missing stage spans: prompt=%v submit=%v", prompt, submit)
	}
	if !prompt.EndAt.Equal(providerStartedAt) {
		t.Errorf("PROMPT_PREPARE.EndAt = %s; want PROVIDER_REQUEST start %s (so the tail bar stops growing)", prompt.EndAt, providerStartedAt)
	}
	if got := prompt.Attributes["end_synthesized"]; got != "next_stage_start" {
		t.Errorf("PROMPT_PREPARE.end_synthesized = %q; want next_stage_start", got)
	}
	if !submit.StartAt.Equal(providerStartedAt) {
		t.Errorf("PROVIDER_SUBMIT.StartAt = %s; want earliest child %s", submit.StartAt, providerStartedAt)
	}
	if !submit.EndAt.Equal(now) {
		t.Errorf("PROVIDER_SUBMIT.EndAt = %s; want now %s (still in flight)", submit.EndAt, now)
	}
	if got := submit.Attributes[endSynthesizedAttr]; got != "in_flight" {
		t.Errorf("PROVIDER_SUBMIT.%s = %q; want in_flight", endSynthesizedAttr, got)
	}
	if submit.Status != "PENDING" {
		t.Errorf("PROVIDER_SUBMIT.Status = %q; want PENDING", submit.Status)
	}
}

// With no children yet (FSM transitioned but no PROVIDER_REQUEST written),
// the synthetic span anchors to the job's UpdatedAt — the FSM-transition
// moment is the best available start.
func TestRunningStageSpanFallsBackToUpdatedAtWithNoChildren(t *testing.T) {
	fsmAdvancedAt := time.Date(2026, 5, 17, 20, 51, 50, 0, time.UTC)
	now := fsmAdvancedAt.Add(3 * time.Second)
	summary := JobSummary{
		Status:       "RUNNING",
		CurrentStage: "PROVIDER_SUBMIT",
		UpdatedAt:    fsmAdvancedAt,
	}
	extra, ok := runningStageSpan(summary, nil, now)
	if !ok {
		t.Fatal("runningStageSpan ok=false; want synthetic stage with UpdatedAt fallback")
	}
	if !extra.StartAt.Equal(fsmAdvancedAt) {
		t.Errorf("StartAt = %s; want UpdatedAt %s", extra.StartAt, fsmAdvancedAt)
	}
	if !extra.EndAt.Equal(now) {
		t.Errorf("EndAt = %s; want now %s", extra.EndAt, now)
	}
}

// Terminal jobs and in-flight retries (where an ATTEMPT for current_stage
// already exists) must not get a synthetic span — derived spans already
// cover them and closeStageEnds owns retry annotations.
func TestRunningStageSpanSkipsWhenNotNeeded(t *testing.T) {
	now := time.Date(2026, 5, 17, 20, 51, 50, 0, time.UTC)
	for _, status := range []string{"COMPLETE", "FAILED", "CANCELLED"} {
		summary := JobSummary{Status: status, CurrentStage: "PROVIDER_SUBMIT", UpdatedAt: now}
		if _, ok := runningStageSpan(summary, nil, now); ok {
			t.Errorf("status %s: ok=true, want false", status)
		}
	}
	terminal := JobSummary{Status: "RUNNING", CurrentStage: "TERMINAL", UpdatedAt: now}
	if _, ok := runningStageSpan(terminal, nil, now); ok {
		t.Errorf("CurrentStage=TERMINAL: ok=true, want false")
	}
	retrying := JobSummary{Status: "RUNNING", CurrentStage: "PROVIDER_SUBMIT", UpdatedAt: now}
	withExistingAttempt := []TraceSpan{
		{ID: "attempt:PROVIDER_SUBMIT:v1:a1", Kind: "ATTEMPT", Stage: "PROVIDER_SUBMIT", Status: "TRANSIENT_FAIL",
			AttemptNo: 1, StartAt: now, EndAt: now},
	}
	if _, ok := runningStageSpan(retrying, withExistingAttempt, now.Add(time.Second)); ok {
		t.Errorf("retry with existing ATTEMPT: ok=true, want false (closeStageEnds owns retry annotations)")
	}
}

func TestDeriveStageSpansCopiesTraceAttributes(t *testing.T) {
	spans := deriveStageSpans([]TraceSpan{{
		ID:      "attempt:INPUT_MODERATION:v1:a1",
		Kind:    "ATTEMPT",
		Status:  "OK",
		Stage:   "INPUT_MODERATION",
		StartAt: time.Date(2026, 5, 17, 2, 31, 53, 0, time.UTC),
		EndAt:   time.Date(2026, 5, 17, 2, 31, 53, 0, time.UTC),
		Attributes: map[string]string{
			"trace_id":    "4bf92f3577b34da6a3ce929d0e0e4736",
			"traceparent": "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		},
	}})

	if len(spans) != 1 {
		t.Fatalf("len(spans) = %d, want 1", len(spans))
	}
	if spans[0].Attributes["trace_id"] != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("trace_id = %q, want copied trace id", spans[0].Attributes["trace_id"])
	}
	if spans[0].Attributes["traceparent"] == "" {
		t.Fatalf("traceparent attribute missing")
	}
}
