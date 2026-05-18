package generation_test

import (
	"context"
	"sync"
	"testing"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	safetyapp "github.com/dtszwai/media-processing-service/backend/internal/app/safety"
	domainaudit "github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/simulated"
)

// recordingRecorder captures every Event a Recorder writes so tests can
// assert the moderation stage emits the expected audit shape without
// reaching into the DDB row layout. Concurrency-safe because both stage
// handlers may emit events from goroutines in larger integration tests.
type recordingRecorder struct {
	mu     sync.Mutex
	events []domainaudit.Event
}

func (r *recordingRecorder) Record(_ context.Context, ev domainaudit.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
	return nil
}

func (r *recordingRecorder) all() []domainaudit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domainaudit.Event, len(r.events))
	copy(out, r.events)
	return out
}

func (r *recordingRecorder) byType(t string) []domainaudit.Event {
	var out []domainaudit.Event
	for _, ev := range r.all() {
		if ev.EventType == t {
			out = append(out, ev)
		}
	}
	return out
}

// stubModerator returns a fixed verdict so tests can drive PASS / FAIL /
// REVIEW branches without depending on the SimulatedModerator's sentinel
// substring (which lives in app/safety and is tested independently).
type stubModerator struct {
	decision safety.Decision
	reason   string
	err      error
	calls    int
	mu       sync.Mutex
}

func (m *stubModerator) Moderate(_ context.Context, in safetyapp.ModerateInput) (safety.Verdict, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	if m.err != nil {
		return safety.Verdict{}, m.err
	}
	return safety.Verdict{
		ID:            "vd-stub",
		TenantID:      in.TenantID,
		Layer:         in.Layer,
		Decision:      m.decision,
		Provider:      "stub",
		Model:         "stub-v1",
		PolicyVersion: "stub-v1",
		ReasonCode:    m.reason,
		CreatedAt:     time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC),
	}, nil
}

func newModerationJob(id string) generation.Job {
	return generation.Job{
		ID:         id,
		TenantID:   "tenant-mod",
		MediaID:    "med-mod",
		OutputType: generation.OutputImage,
		Tier:       generation.TierFree,
		Status:     generation.StatusRunning,
		Prompt:     "a placid landscape painting",
		Model:      "simulated-v1",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
}

// TestStageInputModeration_Pass_AdvancesToBudgetReserve walks the entry
// stage on a clean prompt: the moderator returns PASS, the audit row is
// emitted, and the stage returns the pass outcome consumed by the workflow
// transition resolver.
func TestStageInputModeration_Pass_AdvancesToBudgetReserve(t *testing.T) {
	repo := gen.NewMemRepo()
	wf := newTestWorkflow(t, repo, simulated.New(), gen.NewMemIdempotency(), gen.NewMemSink())
	mod := &stubModerator{decision: safety.DecisionPass}
	rec := &recordingRecorder{}
	wf.Moderator = mod
	wf.AuditRecorder = rec

	ctx := context.Background()
	job := newModerationJob("gen_mod_pass")
	job.CurrentStage = generation.StageInputModeration
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage: %v", err)
	}
	if result.Outcome != gen.OutcomeModerationPassed {
		t.Fatalf("Outcome = %s, want MODERATION_PASSED", result.Outcome)
	}
	if mod.calls != 1 {
		t.Fatalf("moderator calls = %d, want 1", mod.calls)
	}
	events := rec.byType(domainaudit.EventSafetyInputModerationDecided)
	if len(events) != 1 {
		t.Fatalf("input-moderation audit rows = %d, want 1", len(events))
	}
	if events[0].Decision != domainaudit.DecisionPass {
		t.Fatalf("audit decision = %s, want PASS", events[0].Decision)
	}
}

// TestStageInputModeration_Fail_BlocksWithoutBudgetReservation drives the
// first stage on a prompt that fails moderation. The job must terminate
// with SAFETY_BLOCKED, no provider must be called, and — critically — the
// budget reserver MUST NOT see a Reserve call.
func TestStageInputModeration_Fail_BlocksWithoutBudgetReservation(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &countingProvider{}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())
	budget := &countingQuota{}
	wf.QuotaReserver = budget
	wf.Moderator = &stubModerator{decision: safety.DecisionFail, reason: "POLICY_PROHIBITED"}
	rec := &recordingRecorder{}
	wf.AuditRecorder = rec

	ctx := context.Background()
	job := newModerationJob("gen_mod_fail")
	job.CurrentStage = generation.StageInputModeration
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := wf.Run(ctx, job.ID); err == nil {
		t.Fatalf("expected terminal SAFETY_BLOCKED, got nil")
	} else if !generation.IsTerminal(err) {
		t.Fatalf("error not terminal: %v", err)
	} else if got := generation.AsError(err).Code; got != "SAFETY_BLOCKED" {
		t.Fatalf("terminal code = %q, want SAFETY_BLOCKED", got)
	}

	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusFailed {
		t.Fatalf("status = %s, want FAILED", got.Status)
	}
	if prov.calls.Load() != 0 {
		t.Fatalf("provider was called %d times; INPUT_MODERATION FAIL must not reach the provider", prov.calls.Load())
	}
	// AGENTS.md "atomic budget reservation" — no Reserve on the fail
	// path. Reserving here would let attackers drain a tenant cap with
	// disallowed prompts.
	if budget.reserves.Load() != 0 {
		t.Fatalf("budget Reserve called %d times; INPUT_MODERATION FAIL must skip budget reservation", budget.reserves.Load())
	}
	events := rec.byType(domainaudit.EventSafetyInputModerationDecided)
	if len(events) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(events))
	}
	if events[0].Decision != domainaudit.DecisionFail {
		t.Fatalf("audit decision = %s, want FAIL", events[0].Decision)
	}
}

// TestStageInputModeration_Review_BlocksWithoutBudgetReservation mirrors
// the FAIL path for the REVIEW verdict — review queues must not pay for
// provider work either, even though the prompt is not outright rejected.
func TestStageInputModeration_Review_BlocksWithoutBudgetReservation(t *testing.T) {
	repo := gen.NewMemRepo()
	prov := &countingProvider{}
	wf := newTestWorkflow(t, repo, prov, gen.NewMemIdempotency(), gen.NewMemSink())
	budget := &countingQuota{}
	wf.QuotaReserver = budget
	wf.Moderator = &stubModerator{decision: safety.DecisionReview, reason: "BORDERLINE"}

	ctx := context.Background()
	job := newModerationJob("gen_mod_review")
	job.CurrentStage = generation.StageInputModeration
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := wf.Run(ctx, job.ID); !generation.IsTerminal(err) {
		t.Fatalf("expected terminal SAFETY_BLOCKED for REVIEW, got %v", err)
	}
	if budget.reserves.Load() != 0 {
		t.Fatalf("REVIEW must not reserve budget; reserves=%d", budget.reserves.Load())
	}
	if prov.calls.Load() != 0 {
		t.Fatalf("REVIEW must not call provider; calls=%d", prov.calls.Load())
	}
}

// TestStageOutputModeration_Pass_AdvancesToPostprocess walks an already-
// staged job through OUTPUT_MODERATION on a PASS verdict and confirms the
// FSM advances to DISCLOSURE_POSTPROCESS. Uses a permissive default moderator wired
// after the inference stage runs (a moderator that fails on input would
// short-circuit before output moderation is reached).
func TestStageOutputModeration_Pass_AdvancesToPostprocess(t *testing.T) {
	repo := gen.NewMemRepo()
	idem := gen.NewMemIdempotency()
	sink := gen.NewMemSink()
	sink.NextAssetID = "ast-mod-pass"
	wf := newTestWorkflow(t, repo, simulated.New(), idem, sink)
	wf.Moderator = &stubModerator{decision: safety.DecisionPass}
	rec := &recordingRecorder{}
	wf.AuditRecorder = rec

	ctx := context.Background()
	job := newModerationJob("gen_out_pass")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := wf.Run(ctx, job.ID); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusComplete {
		t.Fatalf("status = %s, want COMPLETE", got.Status)
	}
	if got.ResultAssetID != "ast-mod-pass" {
		t.Fatalf("ResultAssetID = %q, want ast-mod-pass", got.ResultAssetID)
	}
	// Both input + output moderation produced audit rows.
	inputEvents := rec.byType(domainaudit.EventSafetyInputModerationDecided)
	outputEvents := rec.byType(domainaudit.EventSafetyOutputModerationDecided)
	if len(inputEvents) != 1 {
		t.Fatalf("input-moderation audit rows = %d, want 1", len(inputEvents))
	}
	if len(outputEvents) != 1 {
		t.Fatalf("output-moderation audit rows = %d, want 1", len(outputEvents))
	}
}

// TestStageOutputModeration_Fail_BlocksBeforePostprocess drives the FSM to
// OUTPUT_MODERATION where the moderator returns FAIL. The job terminates
// with SAFETY_BLOCKED and DISCLOSURE_POSTPROCESS never runs, so the final sink stays
// empty (a fail at OUTPUT_MODERATION must NOT publish customer-visible
// bytes regardless of disclosure markers).
func TestStageOutputModeration_Fail_BlocksBeforePostprocess(t *testing.T) {
	repo := gen.NewMemRepo()
	idem := gen.NewMemIdempotency()
	sink := gen.NewMemSink()
	wf := newTestWorkflow(t, repo, simulated.New(), idem, sink)
	switchingMod := &switchingModerator{
		input:  safety.DecisionPass,
		output: safety.DecisionFail,
		reason: "OUTPUT_DISALLOWED",
	}
	wf.Moderator = switchingMod
	rec := &recordingRecorder{}
	wf.AuditRecorder = rec

	ctx := context.Background()
	job := newModerationJob("gen_out_fail")
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	if err := wf.Run(ctx, job.ID); err == nil {
		t.Fatalf("expected terminal SAFETY_BLOCKED at OUTPUT_MODERATION, got nil")
	} else if !generation.IsTerminal(err) {
		t.Fatalf("error not terminal: %v", err)
	} else if got := generation.AsError(err).Code; got != "SAFETY_BLOCKED" {
		t.Fatalf("terminal code = %q, want SAFETY_BLOCKED", got)
	}
	got, _ := repo.GetJob(ctx, "", job.ID)
	if got.Status != generation.StatusFailed {
		t.Fatalf("status = %s, want FAILED", got.Status)
	}
	// Final sink must not have been written.
	if len(sink.Stored) != 0 {
		t.Fatalf("sink stored %d artifacts; OUTPUT_MODERATION FAIL must not publish", len(sink.Stored))
	}
	// Both audit rows still land — input PASS + output FAIL.
	inputEvents := rec.byType(domainaudit.EventSafetyInputModerationDecided)
	outputEvents := rec.byType(domainaudit.EventSafetyOutputModerationDecided)
	if len(inputEvents) != 1 || inputEvents[0].Decision != domainaudit.DecisionPass {
		t.Fatalf("input audit = %+v, want one PASS row", inputEvents)
	}
	if len(outputEvents) != 1 || outputEvents[0].Decision != domainaudit.DecisionFail {
		t.Fatalf("output audit = %+v, want one FAIL row", outputEvents)
	}
}

// TestStageInputModeration_NoModerator_PermissiveDefault confirms the
// Workflow's no-Moderator path returns a synthetic PASS verdict. Production
// wiring rejects this configuration via ValidateProduction, but the test
// path relies on permissive defaults so existing tests don't need a stub.
func TestStageInputModeration_NoModerator_PermissiveDefault(t *testing.T) {
	repo := gen.NewMemRepo()
	wf := newTestWorkflow(t, repo, simulated.New(), gen.NewMemIdempotency(), gen.NewMemSink())
	// Moderator deliberately nil.

	ctx := context.Background()
	job := newModerationJob("gen_no_mod")
	job.CurrentStage = generation.StageInputModeration
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}

	result, err := wf.RunStage(ctx, &job)
	if err != nil {
		t.Fatalf("RunStage: %v", err)
	}
	if result.Outcome != gen.OutcomeModerationPassed {
		t.Fatalf("Outcome = %s, want MODERATION_PASSED", result.Outcome)
	}
}

// switchingModerator yields different verdicts per layer so the same
// Workflow can exercise input PASS → output FAIL without spinning up two
// moderator instances.
type switchingModerator struct {
	input  safety.Decision
	output safety.Decision
	reason string
}

func (m *switchingModerator) Moderate(_ context.Context, in safetyapp.ModerateInput) (safety.Verdict, error) {
	decision := m.input
	if in.Layer == safety.LayerOutputModeration {
		decision = m.output
	}
	return safety.Verdict{
		ID:            "vd-switch",
		TenantID:      in.TenantID,
		Layer:         in.Layer,
		Decision:      decision,
		Provider:      "switch",
		Model:         "switch-v1",
		PolicyVersion: "switch-v1",
		ReasonCode:    m.reason,
		CreatedAt:     time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC),
	}, nil
}

// countingQuota records every Reserve so tests can prove a moderation FAIL
// short-circuits before cost reservation. Ensure / Commit / Release
// stay no-ops because the failing stage never reaches them.
type countingQuota struct {
	reserves atomicInt32
}

type atomicInt32 struct {
	mu sync.Mutex
	v  int32
}

func (a *atomicInt32) Add(d int32) { a.mu.Lock(); a.v += d; a.mu.Unlock() }
func (a *atomicInt32) Load() int32 { a.mu.Lock(); defer a.mu.Unlock(); return a.v }

func (b *countingQuota) Ensure(_ context.Context, _, _ string) error { return nil }
func (b *countingQuota) Reserve(_ context.Context, _, _ string, _ int64) (bool, int64, error) {
	b.reserves.Add(1)
	return true, 999_999, nil
}
func (b *countingQuota) Commit(_ context.Context, _, _ string, _ int64) error  { return nil }
func (b *countingQuota) Release(_ context.Context, _, _ string, _ int64) error { return nil }

// Recorder interface assertion — drift here surfaces at compile time
// rather than from a passing test that silently bypasses the wiring.
var _ auditapp.Recorder = (*recordingRecorder)(nil)
