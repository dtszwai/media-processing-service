package ops

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

const (
	workerPrecheckStage    = "WORKER_PRECHECK"
	retryStuckAfter        = 30 * time.Minute
	retryStateAttr         = "retry_state"
	retryStateRetrying     = "retrying"
	retryStateAwaiting     = "awaiting_redelivery"
	retryStateStuck        = "stuck"
	lastRetryAttemptAtAttr = "last_retry_attempt_at"
	lastRetryAttemptNoAttr = "last_retry_attempt_no"
	automaticRetryAttr     = "automatic_retry"
	endSynthesizedAttr     = "end_synthesized"
)

func attemptSpan(row map[string]any) TraceSpan {
	recordedStage := stringAttr(row, "stage")
	stage := recordedStage
	attempt := int32(intAttr(row, "attempt_no"))
	result := stringAttr(row, "result")
	errorCode := stringAttr(row, "error_code")
	status := "OK"
	switch result {
	case "TRANSIENT_FAILURE":
		status = "TRANSIENT_FAIL"
	case "TERMINAL_FAILURE":
		status = "TERMINAL_FAIL"
	}
	label := fmt.Sprintf("attempt #%d", attempt)
	if isWorkerPrecheckFailure(errorCode) {
		stage = workerPrecheckStage
		label = "provider resolution"
	}
	started := timeAttr(row, "created_at")
	span := TraceSpan{
		ID:            fmt.Sprintf("attempt:%s:v%d:a%d", stage, intAttr(row, "stage_version"), attempt),
		Kind:          "ATTEMPT",
		Label:         label,
		Status:        status,
		Stage:         stage,
		ResourceClass: stringAttr(row, "resource_class"),
		AttemptNo:     attempt,
		ErrorCode:     errorCode,
		ErrorMessage:  stringAttr(row, "error_message"),
		StartAt:       started,
		EndAt:         started,
		Attributes: map[string]string{
			"next_stage":     stringAttr(row, "next_stage"),
			"resource_class": stringAttr(row, "resource_class"),
		},
		PK: stringAttr(row, "PK"),
		SK: stringAttr(row, "SK"),
	}
	if stage != recordedStage {
		span.Attributes["recorded_stage"] = recordedStage
	}
	if traceparent := stringAttr(row, "traceparent"); traceparent != "" {
		span.Attributes["traceparent"] = traceparent
	}
	if traceID := stringAttr(row, "trace_id"); traceID != "" {
		span.Attributes["trace_id"] = traceID
	}
	return span
}

func providerSpan(row map[string]any) TraceSpan {
	rid := stringAttr(row, "provider_request_id")
	created := timeAttr(row, "created_at")
	updated := timeAttr(row, "updated_at")
	completed := timeAttr(row, "completed_at")
	end := updated
	if !completed.IsZero() {
		end = completed
	}
	status := stringAttr(row, "status")
	mappedStatus := "PENDING"
	switch status {
	case "SUCCEEDED":
		mappedStatus = "OK"
	case "FAILED":
		mappedStatus = "TERMINAL_FAIL"
	}
	span := TraceSpan{
		ID:           "provider:" + rid,
		Kind:         "PROVIDER_REQUEST",
		Label:        fmt.Sprintf("%s · %s", stringAttr(row, "provider"), stringAttr(row, "call_type")),
		Status:       mappedStatus,
		ErrorCode:    stringAttr(row, "error_code"),
		ErrorMessage: stringAttr(row, "error_message"),
		StartAt:      created,
		EndAt:        end,
		Attributes: map[string]string{
			"provider":          stringAttr(row, "provider"),
			"model":             stringAttr(row, "model"),
			"call_type":         stringAttr(row, "call_type"),
			"request_hash":      stringAttr(row, "request_hash"),
			"vendor_request_id": stringAttr(row, "vendor_request_id"),
			"provider_job_id":   stringAttr(row, "provider_job_id"),
			"status":            status,
		},
		PK: stringAttr(row, "PK"),
		SK: stringAttr(row, "SK"),
	}
	if !end.IsZero() && !created.IsZero() {
		span.DurationMS = end.Sub(created).Milliseconds()
	}
	span.Stage = string(generation.StageProviderSubmit)
	return span
}

func terminalSpan(row map[string]any) TraceSpan {
	status := stringAttr(row, "status")
	mapped := "OK"
	switch status {
	case "FAILED":
		mapped = "TERMINAL_FAIL"
	case "CANCELLED":
		mapped = "TERMINAL_FAIL"
	}
	created := timeAttr(row, "created_at")
	return TraceSpan{
		ID:           "terminal",
		Kind:         "TERMINAL",
		Label:        "terminal · " + strings.ToLower(status),
		Status:       mapped,
		Stage:        string(generation.StageTerminal),
		StartAt:      created,
		EndAt:        created,
		ErrorCode:    stringAttr(row, "error_code"),
		ErrorMessage: stringAttr(row, "error_message"),
		Attributes:   map[string]string{"status": status},
		PK:           stringAttr(row, "PK"),
		SK:           stringAttr(row, "SK"),
	}
}

func outputSpan(row map[string]any) TraceSpan {
	created := timeAttr(row, "created_at")
	updated := timeAttr(row, "updated_at")
	if updated.IsZero() {
		updated = created
	}
	completed := timeAttr(row, "completed_at")
	status := stringAttr(row, "status")
	start := created
	end := updated
	if outputStatus(status) != "PENDING" {
		start = updated
		end = updated
		if !completed.IsZero() {
			start = completed
			end = completed
		}
	}
	return TraceSpan{
		ID:      "output:" + stringAttr(row, "output_id"),
		Kind:    "OUTPUT",
		Label:   "output record · " + status,
		Status:  outputStatus(status),
		Stage:   string(generation.StagePublish),
		StartAt: start,
		EndAt:   end,
		Attributes: map[string]string{
			"output_id": stringAttr(row, "output_id"),
			"type":      stringAttr(row, "type"),
			"status":    status,
		},
		PK: stringAttr(row, "PK"),
		SK: stringAttr(row, "SK"),
	}
}

func outputStatus(status string) string {
	switch strings.ToUpper(status) {
	case string(generation.StatusComplete), "READY", "SUCCEEDED":
		return "OK"
	case string(generation.StatusFailed), string(generation.StatusCancelled):
		return "TERMINAL_FAIL"
	default:
		return "PENDING"
	}
}

func variantSpan(row map[string]any) TraceSpan {
	created := timeAttr(row, "created_at")
	updated := timeAttr(row, "updated_at")
	if updated.IsZero() {
		updated = created
	}
	return TraceSpan{
		ID:      "variant:" + stringAttr(row, "variant_id"),
		Kind:    "VARIANT",
		Label:   fmt.Sprintf("variant #%d", intAttr(row, "index")),
		Status:  "OK",
		Stage:   string(generation.StagePublish),
		StartAt: created,
		EndAt:   updated,
		Attributes: map[string]string{
			"variant_id":     stringAttr(row, "variant_id"),
			"final_asset_id": stringAttr(row, "final_asset_id"),
			"provider":       stringAttr(row, "provider"),
			"model":          stringAttr(row, "model"),
			"mime":           stringAttr(row, "mime"),
		},
		PK: stringAttr(row, "PK"),
		SK: stringAttr(row, "SK"),
	}
}

// runningStageSpan synthesizes a STAGE span for the job's current_stage when
// the worker is actively running it. STAGE spans normally derive from
// STAGE_ATTEMPT rows, but AdvanceStageAndEnqueue writes those rows only at
// the *end* of a stage's work — so while a stage is mid-flight the trace has
// no STAGE row to anchor it. Without this, the PROVIDER_REQUEST child floats
// orphaned and the previously-completed stage becomes the visible tail,
// drifting toward `now` as closeStageEnds falls into its default branch.
func runningStageSpan(summary JobSummary, spans []TraceSpan, now time.Time) (TraceSpan, bool) {
	stage := summary.CurrentStage
	if stage == "" || stage == string(generation.StageTerminal) {
		return TraceSpan{}, false
	}
	switch summary.Status {
	case string(generation.StatusComplete),
		string(generation.StatusFailed),
		string(generation.StatusCancelled):
		return TraceSpan{}, false
	}
	var earliestChild time.Time
	for _, s := range spans {
		if s.Stage != stage {
			continue
		}
		if s.Kind == "STAGE" || s.Kind == "ATTEMPT" {
			return TraceSpan{}, false
		}
		if s.StartAt.IsZero() {
			continue
		}
		if earliestChild.IsZero() || s.StartAt.Before(earliestChild) {
			earliestChild = s.StartAt
		}
	}
	start := earliestChild
	if start.IsZero() {
		start = summary.UpdatedAt
	}
	if start.IsZero() {
		return TraceSpan{}, false
	}
	return TraceSpan{
		ID:      "stage:" + stage,
		Kind:    "STAGE",
		Label:   stage,
		Status:  "PENDING",
		Stage:   stage,
		StartAt: start,
		EndAt:   now,
		Attributes: map[string]string{
			// Distinct from "now"/"next_stage_start" tags closeStageEnds
			// applies — this stage is provably in flight, not a tail
			// projection. closeStageEnds clamps any negative duration.
			endSynthesizedAttr: "in_flight",
		},
	}, true
}

func deriveStageSpans(spans []TraceSpan) []TraceSpan {
	byStage := map[string]*TraceSpan{}
	order := []string{}
	for _, s := range spans {
		if s.Kind != "ATTEMPT" || s.Stage == "" {
			continue
		}
		cur := byStage[s.Stage]
		if cur == nil {
			cur = &TraceSpan{
				ID:            "stage:" + s.Stage,
				Kind:          "STAGE",
				Label:         s.Stage,
				Status:        s.Status,
				Stage:         s.Stage,
				ResourceClass: s.ResourceClass,
				StartAt:       s.StartAt,
				EndAt:         s.EndAt,
				Attributes:    map[string]string{},
			}
			byStage[s.Stage] = cur
			order = append(order, s.Stage)
		}
		if s.StartAt.Before(cur.StartAt) || cur.StartAt.IsZero() {
			cur.StartAt = s.StartAt
		}
		if s.EndAt.After(cur.EndAt) {
			cur.EndAt = s.EndAt
		}
		cur.Status = worseStatus(cur.Status, s.Status)
		if s.ErrorCode != "" {
			cur.ErrorCode = s.ErrorCode
			cur.ErrorMessage = s.ErrorMessage
		}
		copyAttrIfPresent(cur.Attributes, s.Attributes, "trace_id")
		copyAttrIfPresent(cur.Attributes, s.Attributes, "traceparent")
	}
	out := make([]TraceSpan, 0, len(order))
	for _, st := range order {
		span := byStage[st]
		if span.EndAt.IsZero() {
			span.EndAt = span.StartAt
		}
		if !span.StartAt.IsZero() && !span.EndAt.IsZero() {
			span.DurationMS = span.EndAt.Sub(span.StartAt).Milliseconds()
		}
		out = append(out, *span)
	}
	return out
}

func copyAttrIfPresent(dst, src map[string]string, key string) {
	if dst[key] != "" {
		return
	}
	if v := src[key]; v != "" {
		dst[key] = v
	}
}

// closeStageEnds chooses each STAGE's EndAt against a priority of signals:
//
//  1. Observable child events (PROVIDER_REQUEST.completed_at,
//     OUTPUT/VARIANT.updated_at). These are real wall-clock end-of-work
//     timestamps and always win when they fall after the stage start.
//  2. The multi-attempt range already produced by deriveStageSpans
//     (StartAt..max ATTEMPT.created_at) — preserved when no provider/output
//     child is later.
//  3. A latest TRANSIENT_FAIL attempt. That attempt is an immutable outcome
//     event, so the stage should stop there instead of drifting to now while
//     it waits for SQS redelivery.
//  4. The next stage's StartAt as a last-resort projection. This is the path
//     that historically conflated queue handoff with stage work — we still
//     use it when nothing better exists, but stamp end_synthesized on the
//     attributes so the UI can render those bars as projections rather than
//     as observed durations.
//  5. firstClosingEventAfter / now for the tail stage with no successor.
//
// DurationMS is guarded against negatives — out-of-order writes from the FSM
// would otherwise surface as negative bars in the waterfall.
func closeStageEnds(spans []TraceSpan, now time.Time) {
	childIdx := map[string][]int{}
	for i, s := range spans {
		if s.Kind == "STAGE" || s.Stage == "" {
			continue
		}
		childIdx[s.Stage] = append(childIdx[s.Stage], i)
	}

	stages := []int{}
	for i, s := range spans {
		if s.Kind == "STAGE" {
			stages = append(stages, i)
		}
	}

	// Pass 1: rewrite every stage's StartAt against observable child
	// signals. deriveStageSpans seeds a stage's StartAt from its ATTEMPT
	// row, which fires when AdvanceStageAndEnqueue runs *at the end* of
	// the stage's work. Pass 2's "next stage's start" projection reads
	// the next stage's StartAt directly, so without correcting every
	// stage first that projection picks up the next stage's attempt
	// timestamp (the moment its work *finished*) and the synthesized bar
	// blows out to span the next stage's entire wall-clock.
	for _, idx := range stages {
		stage := &spans[idx]
		if stage.Attributes == nil {
			stage.Attributes = map[string]string{}
		}
		children := childIdx[stage.Stage]
		if observedStart := observedStageStart(spans, children); !observedStart.IsZero() &&
			(stage.StartAt.IsZero() || observedStart.Before(stage.StartAt)) {
			stage.StartAt = observedStart
		}
	}

	sort.Slice(stages, func(i, j int) bool {
		return spans[stages[i]].StartAt.Before(spans[stages[j]].StartAt)
	})

	// Pass 2: fill EndAt using the corrected StartAts as anchors.
	for i, idx := range stages {
		stage := &spans[idx]
		children := childIdx[stage.Stage]

		observed := observedStageEnd(spans, children)
		latestAttempt := latestStageAttempt(spans, children)
		synthesized := ""

		switch {
		case !observed.IsZero() && observed.After(stage.StartAt):
			stage.EndAt = observed
		case stage.EndAt.After(stage.StartAt):
			// Multi-attempt range from deriveStageSpans is already meaningful.
		case latestAttempt != nil && latestAttempt.Status == "TRANSIENT_FAIL" && !hasPendingChildAfter(spans, children, spanEventTime(*latestAttempt)):
			if attemptAt := spanEventTime(*latestAttempt); !attemptAt.IsZero() {
				stage.EndAt = attemptAt
			}
		case i+1 < len(stages):
			next := spans[stages[i+1]]
			if next.StartAt.After(stage.StartAt) {
				stage.EndAt = next.StartAt
				synthesized = "next_stage_start"
			}
		default:
			if closed := firstClosingEventAfter(spans, idx); !closed.IsZero() {
				stage.EndAt = closed
			} else if now.After(stage.StartAt) {
				stage.EndAt = now
				synthesized = "now"
			}
		}

		if synthesized != "" {
			stage.Attributes[endSynthesizedAttr] = synthesized
		}
		annotateRetryState(stage, latestAttempt, spans, children, now)

		if !stage.EndAt.IsZero() && !stage.StartAt.IsZero() {
			d := stage.EndAt.Sub(stage.StartAt).Milliseconds()
			if d < 0 {
				d = 0
				stage.EndAt = stage.StartAt
			}
			stage.DurationMS = d
		}
	}
}

func observedStageStart(spans []TraceSpan, childIdxs []int) time.Time {
	var best time.Time
	for _, i := range childIdxs {
		c := spans[i]
		if !isTerminalProviderRequest(c) {
			continue
		}
		if c.StartAt.IsZero() {
			continue
		}
		if best.IsZero() || c.StartAt.Before(best) {
			best = c.StartAt
		}
	}
	return best
}

// observedStageEnd returns the latest end-of-work signal across a stage's
// non-attempt children. ATTEMPT rows are point-in-time outcome events
// (created_at == updated_at == the moment AdvanceStageAndEnqueue ran), so
// they record the FSM transition, not when the stage's work actually
// stopped. PROVIDER_REQUEST / OUTPUT / VARIANT / audit rows carry real
// end-of-work timestamps (or insertion markers that are always after the
// owning stage started).
func observedStageEnd(spans []TraceSpan, childIdxs []int) time.Time {
	var best time.Time
	for _, i := range childIdxs {
		c := spans[i]
		if c.Kind == "ATTEMPT" {
			continue
		}
		if c.Kind == "PROVIDER_REQUEST" && !isTerminalProviderRequest(c) {
			continue
		}
		if c.StartAt.IsZero() {
			continue
		}
		t := c.EndAt
		if t.IsZero() {
			t = c.StartAt
		}
		if t.After(best) {
			best = t
		}
	}
	return best
}

func firstClosingEventAfter(spans []TraceSpan, stageIdx int) time.Time {
	stage := spans[stageIdx]
	var best time.Time
	for i, s := range spans {
		if i == stageIdx || s.Kind == "STAGE" || !isClosingEventSpan(s) {
			continue
		}
		t := s.StartAt
		if s.EndAt.After(t) {
			t = s.EndAt
		}
		if t.IsZero() || t.Before(stage.StartAt) {
			continue
		}
		if best.IsZero() || t.Before(best) {
			best = t
		}
	}
	return best
}

func isClosingEventSpan(s TraceSpan) bool {
	switch s.Kind {
	case "TERMINAL", "TERMINAL_AUDIT", "GATE_AUDIT":
		return true
	case "PROVIDER_REQUEST":
		return isTerminalProviderRequest(s)
	default:
		return false
	}
}

func isTerminalProviderRequest(s TraceSpan) bool {
	if s.Kind != "PROVIDER_REQUEST" || spanEventTime(s).IsZero() {
		return false
	}
	switch strings.ToUpper(s.Attributes["status"]) {
	case "SUCCEEDED", "FAILED":
		return true
	}
	switch s.Status {
	case "OK", "TERMINAL_FAIL":
		return true
	default:
		return false
	}
}

func latestStageAttempt(spans []TraceSpan, childIdxs []int) *TraceSpan {
	var latest *TraceSpan
	for _, i := range childIdxs {
		c := &spans[i]
		if c.Kind != "ATTEMPT" {
			continue
		}
		if latest == nil || c.AttemptNo > latest.AttemptNo ||
			(c.AttemptNo == latest.AttemptNo && spanEventTime(*c).After(spanEventTime(*latest))) {
			latest = c
		}
	}
	return latest
}

func hasPendingChildAfter(spans []TraceSpan, childIdxs []int, after time.Time) bool {
	for _, i := range childIdxs {
		c := spans[i]
		if c.Kind == "ATTEMPT" || c.Status != "PENDING" {
			continue
		}
		t := c.StartAt
		if t.IsZero() {
			t = c.EndAt
		}
		if after.IsZero() || t.IsZero() || !t.Before(after) {
			return true
		}
	}
	return false
}

func annotateRetryState(stage *TraceSpan, latestAttempt *TraceSpan, spans []TraceSpan, childIdxs []int, now time.Time) {
	if latestAttempt == nil || latestAttempt.Status != "TRANSIENT_FAIL" {
		return
	}
	attemptAt := spanEventTime(*latestAttempt)
	if !attemptAt.IsZero() {
		stage.Attributes[lastRetryAttemptAtAttr] = attemptAt.Format(time.RFC3339Nano)
	}
	if latestAttempt.AttemptNo > 0 {
		stage.Attributes[lastRetryAttemptNoAttr] = fmt.Sprintf("%d", latestAttempt.AttemptNo)
	}
	stage.Attributes[automaticRetryAttr] = "true"
	if hasFreshProviderRetryAfter(spans, childIdxs, attemptAt) {
		stage.Attributes[retryStateAttr] = retryStateRetrying
		return
	}
	if !attemptAt.IsZero() && now.Sub(attemptAt) > retryStuckAfter {
		stage.Attributes[retryStateAttr] = retryStateStuck
		return
	}
	stage.Attributes[retryStateAttr] = retryStateAwaiting
}

// hasFreshProviderRetryAfter reports whether the worker started a NEW
// provider call after the most recent failed attempt — the only signal in
// the trace that reliably indicates "a retry is in flight" vs. "we're
// waiting in queue".
//
// Stricter than hasPendingChildAfter on purpose:
//
//   - Only PROVIDER_REQUEST children count. OUTPUT / VARIANT / audit rows
//     can carry timestamps after the attempt for reasons unrelated to a
//     retry (eager output-slot inserts, downstream stage updates).
//   - Terminal provider requests (SUCCEEDED/FAILED) don't count — only ones
//     still in flight do.
//   - A 500ms margin past attemptAt avoids catching the provider request
//     that BELONGED to the just-failed attempt; its created_at can race
//     the attempt outcome row by a few milliseconds.
//
// The looser hasPendingChildAfter is still used by closeStageEnds, where
// the goal is "don't pin the stage end if anything is still moving".
func hasFreshProviderRetryAfter(spans []TraceSpan, childIdxs []int, after time.Time) bool {
	if after.IsZero() {
		return false
	}
	threshold := after.Add(500 * time.Millisecond)
	for _, i := range childIdxs {
		c := spans[i]
		if c.Kind != "PROVIDER_REQUEST" {
			continue
		}
		if isTerminalProviderRequest(c) {
			continue
		}
		t := c.StartAt
		if t.IsZero() {
			t = c.EndAt
		}
		if t.IsZero() {
			continue
		}
		if !t.Before(threshold) {
			return true
		}
	}
	return false
}

func spanEventTime(s TraceSpan) time.Time {
	if !s.EndAt.IsZero() {
		return s.EndAt
	}
	return s.StartAt
}

func linkChildrenToStages(spans []TraceSpan) {
	for i, s := range spans {
		if s.Kind == "ATTEMPT" && s.Stage != "" {
			spans[i].ParentID = "stage:" + s.Stage
		}
		if s.Kind == "PROVIDER_REQUEST" && s.Stage != "" {
			spans[i].ParentID = "stage:" + s.Stage
		}
		if (s.Kind == "OUTPUT" || s.Kind == "VARIANT") && s.Stage != "" {
			spans[i].ParentID = "stage:" + s.Stage
		}
	}
}

func worseStatus(a, b string) string {
	rank := func(s string) int {
		switch s {
		case "TERMINAL_FAIL":
			return 3
		case "TRANSIENT_FAIL":
			return 2
		case "PENDING":
			return 1
		}
		return 0
	}
	if rank(b) > rank(a) {
		return b
	}
	return a
}

func isWorkerPrecheckFailure(errorCode string) bool {
	return errorCode == "PROVIDER_UNAVAILABLE"
}
