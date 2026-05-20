// Package obs holds the OpenTelemetry instrument set the grafana dashboards
// consume. Concentrating instruments here (instead of letting each call-site
// own its own Meter) gives one place to:
//
//   - audit the cardinality of every attribute key
//   - keep the dashboard JSON in sync with the emitter set (the JSON references
//     names; if a name does not exist on Instruments, the dashboard panel
//     silently flatlines)
//   - hand callers a Noop() so tests can construct production paths without
//     wiring a real Meter
//
// See the dashboard JSON under data/grafana/dashboards/ for the targets that
// consume these names. Instrument units follow the OpenTelemetry unit
// conventions (`ms`, `1`, `%`).
package obs

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/metric/noop"
)

// MeterName is the instrumentation name passed to MeterProvider.Meter. Kept
// here so every call-site that wants to record an ad-hoc metric uses the
// identical name and the SDK keeps a single Meter instance per process.
const MeterName = "media-service-go"

// Instruments bundles the metric instruments emitted on hot paths. Constructed
// once at bootstrap and threaded into the workflow, provider adapters, outbox
// relay, and safety stages. The struct is goroutine-safe — every embedded
// instrument is itself safe for concurrent use, per the OTEL API contract.
type Instruments struct {
	// Workflow counters.
	WorkflowStageStarted      metric.Int64Counter
	WorkflowStageCompleted    metric.Int64Counter
	WorkflowTerminal          metric.Int64Counter
	PromptEnhancementAttempts metric.Int64Counter
	BudgetPreflight           metric.Int64Counter
	SubmitRejected            metric.Int64Counter
	CostReserve               metric.Int64Counter

	// Workflow latency. ms keeps the unit cardinality across providers/work
	// classes consistent — a histogram of seconds would lose resolution on
	// fast stages (claim acquire, validation) that complete in single-digit
	// milliseconds.
	WorkflowStageLatency    metric.Float64Histogram
	WorkflowDispatchLatency metric.Float64Histogram

	// Provider call counters + latency.
	ProviderRequests       metric.Int64Counter
	ProviderRequestLatency metric.Float64Histogram

	// Safety decision counter — emitted once per moderation stage exit.
	SafetyDecisions metric.Int64Counter

	// Outbox relay counters + per-row publish latency.
	OutboxPublished    metric.Int64Counter
	OutboxRelayLatency metric.Float64Histogram
	OutboxPendingAge   metric.Float64Histogram

	// Outbound webhook delivery attempts.
	WebhookDeliveries metric.Int64Counter

	// QuotaUsedPct is observable so its value reflects whatever the
	// underlying reservoir reports at scrape time — pull-based, no
	// per-reservation event needed on the hot path.
	QuotaUsedPct metric.Float64ObservableGauge
}

// Result label values. Synchronous code paths label outcomes with one of
// these; free-form error messages are intentionally never used as label
// values to keep label cardinality bounded. Error codes (already enum-shaped
// via generation.Error.Code) go into a separate `error_code` attribute.
const (
	ResultSuccess       = "success"
	ResultTransientFail = "transient_fail"
	ResultTerminalFail  = "terminal_fail"
)

// Outcome label values for OutboxPublished — same shape as the provider
// result enum but with explicit naming for the relay's per-row decision.
const (
	OutboxResultPublished = "published"
	OutboxResultFailed    = "failed"
	OutboxResultPoisoned  = "poisoned"
)

// Provider call modes. `sync` covers GenerateSync; `submit`/`poll`/`fetch`
// cover the async lifecycle so the latency histogram can distinguish the
// quick submit from the long-tail poll.
const (
	ProviderModeSync   = "sync"
	ProviderModeSubmit = "submit"
	ProviderModePoll   = "poll"
	ProviderModeFetch  = "fetch"
)

// NewInstruments registers every instrument on meter. Failures are wrapped
// with the instrument name so a wiring bug surfaces at the offender, not at
// the first downstream Add() call.
func NewInstruments(meter metric.Meter) (*Instruments, error) {
	if meter == nil {
		return nil, fmt.Errorf("obs: nil meter")
	}
	i := &Instruments{}
	var err error

	if i.WorkflowStageStarted, err = meter.Int64Counter(
		"workflow.stage_started_total",
		metric.WithDescription("Generation workflow stage entry count, by stage and work class."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: workflow.stage_started_total: %w", err)
	}

	if i.WorkflowStageCompleted, err = meter.Int64Counter(
		"workflow.stage_completed_total",
		metric.WithDescription("Generation workflow stage completion count, by stage, result, and error_code."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: workflow.stage_completed_total: %w", err)
	}

	if i.WorkflowTerminal, err = meter.Int64Counter(
		"workflow.terminal_total",
		metric.WithDescription("Generation workflow terminal-status transitions, by status and error_code."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: workflow.terminal_total: %w", err)
	}

	if i.PromptEnhancementAttempts, err = meter.Int64Counter(
		"workflow.prompt_enhancement_attempts_total",
		metric.WithDescription("Prompt enhancement attempts by outcome, output type, and enhancement policy version."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: workflow.prompt_enhancement_attempts_total: %w", err)
	}

	if i.BudgetPreflight, err = meter.Int64Counter(
		"generation.budget_preflight_total",
		metric.WithDescription("Generation submit budget preflight decisions, by outcome."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: generation.budget_preflight_total: %w", err)
	}

	if i.SubmitRejected, err = meter.Int64Counter(
		"generation.submit_rejected_total",
		metric.WithDescription("Generation submits rejected before job creation, by reason."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: generation.submit_rejected_total: %w", err)
	}

	if i.CostReserve, err = meter.Int64Counter(
		"generation.cost_reserve_total",
		metric.WithDescription("Authoritative generation cost reserve outcomes."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: generation.cost_reserve_total: %w", err)
	}

	// Int64 latency captures `time.Since(...).Milliseconds()` cleanly without
	// truncation — Float64 is used because OTEL histogram percentile math is
	// inherently floating-point, but the inputs are millisecond-precise.
	if i.WorkflowStageLatency, err = meter.Float64Histogram(
		"workflow.stage_latency_ms",
		metric.WithDescription("Generation workflow stage wall-clock latency."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("obs: workflow.stage_latency_ms: %w", err)
	}

	if i.WorkflowDispatchLatency, err = meter.Float64Histogram(
		"workflow.dispatch_latency_ms",
		metric.WithDescription("Generation stage dispatch latency from outbox enqueue to worker stage span start. Missing enqueue timestamps are skipped rather than recorded as zero."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("obs: workflow.dispatch_latency_ms: %w", err)
	}

	if i.ProviderRequests, err = meter.Int64Counter(
		"provider.requests_total",
		metric.WithDescription("Provider adapter call count, by provider, model, and result."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: provider.requests_total: %w", err)
	}

	if i.ProviderRequestLatency, err = meter.Float64Histogram(
		"provider.request_latency_ms",
		metric.WithDescription("Provider adapter call latency, by provider, model, and mode (sync/submit/poll/fetch)."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("obs: provider.request_latency_ms: %w", err)
	}

	if i.SafetyDecisions, err = meter.Int64Counter(
		"safety.decisions_total",
		metric.WithDescription("Safety moderation decisions, by layer (INPUT_MODERATION / OUTPUT_MODERATION), decision (PASS/FAIL/REVIEW), and most-flagged category."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: safety.decisions_total: %w", err)
	}

	if i.OutboxPublished, err = meter.Int64Counter(
		"outbox.published_total",
		metric.WithDescription("Outbox relay per-row publish outcomes, by stream, event_type, and result."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: outbox.published_total: %w", err)
	}

	if i.OutboxRelayLatency, err = meter.Float64Histogram(
		"outbox.relay_latency_ms",
		metric.WithDescription("Outbox relay per-row publish wall-clock latency."),
		metric.WithUnit("ms"),
	); err != nil {
		return nil, fmt.Errorf("obs: outbox.relay_latency_ms: %w", err)
	}

	if i.OutboxPendingAge, err = meter.Float64Histogram(
		"outbox.pending_age_seconds",
		metric.WithDescription("Age of pending outbox rows when the relay observes them."),
		metric.WithUnit("s"),
	); err != nil {
		return nil, fmt.Errorf("obs: outbox.pending_age_seconds: %w", err)
	}

	if i.WebhookDeliveries, err = meter.Int64Counter(
		"webhook.deliveries_total",
		metric.WithDescription("Outbound webhook delivery attempts, by result and status code class."),
		metric.WithUnit("1"),
	); err != nil {
		return nil, fmt.Errorf("obs: webhook.deliveries_total: %w", err)
	}

	if i.QuotaUsedPct, err = meter.Float64ObservableGauge(
		"quota.used_pct",
		metric.WithDescription("Reservoir utilisation as (cap - available) / cap, observed at scrape time."),
		metric.WithUnit("%"),
	); err != nil {
		return nil, fmt.Errorf("obs: quota.used_pct: %w", err)
	}

	return i, nil
}

// Noop returns an Instruments backed by the metric/noop MeterProvider. Used by
// test paths that exercise production code which expects a non-nil *Instruments
// but does not want a real exporter. The returned struct is safe for the same
// concurrent use as the production one; every Add/Record is a no-op.
func Noop() *Instruments {
	i, err := NewInstruments(noop.NewMeterProvider().Meter(MeterName))
	if err != nil {
		// noop.Meter never errors on instrument creation; if it ever does the
		// only safe move is panic — there is no other failure mode that
		// preserves the "Noop returns a non-nil *Instruments" contract.
		panic(fmt.Errorf("obs.Noop: %w", err))
	}
	return i
}
