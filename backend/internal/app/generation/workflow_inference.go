package generation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
)

// stageInference dispatches to the sync (InlineBytes) or async branch and on
// success drops the provider's raw output into the Stager so the downstream
// gate stage can re-load and mutate it. See workflow.go for the
// INFER → DISCLOSURE_POSTPROCESS split rationale.
func (w *Workflow) stageInference(ctx context.Context, job *generation.Job) (StageResult, error) {
	if w.Provider == nil {
		return StageResult{}, errors.New("workflow: no provider configured")
	}
	if job.PreparedPromptHash == "" || job.PreparedPrompt == "" {
		return StageResult{}, generation.Terminal("PROMPT_NOT_PREPARED", "provider submit requires prepared prompt")
	}

	if !w.Provider.InlineBytes() {
		return w.stageInferenceAsync(ctx, job)
	}

	scope := stagedScopeFor(job.ID)
	providerName := providerName(w.Provider)
	providerInputHash := hashProviderInput(job, providerName)
	claim, err := w.acquireStageClaim(ctx, scope, providerInputHash, "PROVIDER_SUBMIT")
	if err != nil {
		return StageResult{}, err
	}
	if claim.replayed {
		// Staged artifact already exists from a prior run. Skip the provider
		// call and re-enter OUTPUT_MODERATION so the staged bytes go
		// through the safety gate before any disclosure mutation. The
		// downstream DISCLOSURE_POSTPROCESS handles expired staged refs by failing
		// terminally with STAGED_EXPIRED.
		return w.providerSuccessResult(ctx, job)
	}
	token := claim.token

	spec := generation.JobSpec{
		JobID:           job.ID,
		MediaID:         job.MediaID,
		TenantID:        job.TenantID,
		OutputType:      job.OutputType,
		Provider:        providerName,
		Prompt:          job.PreparedPrompt,
		Model:           job.Model,
		Resolution:      job.Resolution,
		Seed:            job.Seed,
		ClientRequestID: vendorRequestID(job, providerName),
	}
	var (
		art  generation.Artifact
		perr error
	)
	req := w.providerRequest(job, providerName, obs.ProviderModeSync, providerInputHash, spec.ClientRequestID)
	if err := w.putProviderRequest(ctx, req); err != nil {
		w.claimAbandon(ctx, scope, token)
		return StageResult{}, err
	}
	startProv := w.now()
	if w.LeaseRunner != nil {
		ttl := w.LeaseTTL
		if ttl <= 0 {
			ttl = 5 * time.Minute
		}
		perr = w.LeaseRunner.WithLease(ctx, LeaseRequest{
			ResourceClass: generation.ResourceProvider,
			TenantID:      job.TenantID,
			JobID:         job.ID,
			TTL:           ttl,
		}, func(lctx context.Context, _ *ResourceLease) error {
			var inner error
			art, inner = w.Provider.GenerateSync(lctx, spec)
			return inner
		})
	} else {
		art, perr = w.Provider.GenerateSync(ctx, spec)
	}
	w.emitProviderCall(ctx, providerName, job.Model, obs.ProviderModeSync,
		float64(w.now().Sub(startProv))/float64(time.Millisecond), perr)
	if perr != nil {
		_ = w.updateProviderRequest(ctx, job, req.ID, ProviderRequestFailed, "", perr)
		w.claimFailOrAbandon(ctx, scope, token, perr)
		return StageResult{}, perr
	}
	_ = w.updateProviderRequest(ctx, job, req.ID, ProviderRequestSucceeded, "", nil)

	result, err := w.stageArtifactAndCommit(ctx, job, art, token, scope, req.ID)
	if err == nil {
		result.ProviderRequestID = req.ID
	}
	return result, err
}

// stageInferenceAsync handles the InlineBytes=false path. It calls
// SubmitAsync with spec.ClientRequestID as the vendor idempotency key, caches
// the returned provider job id in the stage claim, and transitions to
// StageProviderWait, leaving the budget RESERVED.
func (w *Workflow) stageInferenceAsync(ctx context.Context, job *generation.Job) (StageResult, error) {
	pname := providerName(w.Provider)
	if job.PreparedPromptHash == "" || job.PreparedPrompt == "" {
		return StageResult{}, generation.Terminal("PROMPT_NOT_PREPARED", "provider submit requires prepared prompt")
	}
	providerInputHash := hashProviderInput(job, pname)
	scope := asyncSubmitScopeFor(job.ID)
	claim, err := w.acquireStageClaim(ctx, scope, providerInputHash, "PROVIDER_SUBMIT")
	if err != nil {
		return StageResult{}, err
	}
	if claim.replayed {
		result := w.nextStageResult(ctx, job, generation.StageProviderWait, generation.ResourcePoll)
		result.ProviderJobID = claim.replayResult
		return result, nil
	}
	token := claim.token

	spec := generation.JobSpec{
		JobID:           job.ID,
		MediaID:         job.MediaID,
		TenantID:        job.TenantID,
		OutputType:      job.OutputType,
		Provider:        pname,
		Prompt:          job.PreparedPrompt,
		Model:           job.Model,
		Resolution:      job.Resolution,
		Seed:            job.Seed,
		ClientRequestID: vendorRequestID(job, pname),
	}
	req := w.providerRequest(job, pname, obs.ProviderModeSubmit, providerInputHash, spec.ClientRequestID)
	if err := w.putProviderRequest(ctx, req); err != nil {
		w.claimAbandon(ctx, scope, token)
		return StageResult{}, err
	}
	start := w.now()
	providerJobID, err := w.Provider.SubmitAsync(ctx, spec)
	w.emitProviderCall(ctx, pname, job.Model, obs.ProviderModeSubmit,
		float64(w.now().Sub(start))/float64(time.Millisecond), err)
	if err != nil {
		_ = w.updateProviderRequest(ctx, job, req.ID, ProviderRequestFailed, "", err)
		w.claimFailOrAbandon(ctx, scope, token, err)
		return StageResult{}, err
	}
	_ = w.updateProviderRequest(ctx, job, req.ID, ProviderRequestSubmitted, providerJobID, nil)
	if err := w.claimComplete(ctx, scope, token, providerJobID); err != nil {
		return StageResult{}, fmt.Errorf("workflow: complete async submit claim: %w", err)
	}
	result := w.nextStageResult(ctx, job, generation.StageProviderWait, generation.ResourcePoll)
	result.ProviderJobID = providerJobID
	result.ProviderRequestID = req.ID
	return result, nil
}

// stageInferencePoll drives the async provider until it reports Ready or
// Failed. On Pending it re-enqueues to StageProviderWait (transient path)
// so SQS visibility timeout provides back-off. On Ready it fetches, gates,
// and sinks the artifact before committing to StageDisclosurePostprocess with a
// LedgerUpdateCommitted op. On Failed it transitions to terminal with
// LedgerUpdateReleased.
//
// The handler is idempotent by construction: re-running after a crash just
// re-polls the provider; polling is free and has no side-effects.
func (w *Workflow) stageInferencePoll(ctx context.Context, job *generation.Job) (StageResult, error) {
	if w.Provider == nil {
		return StageResult{}, errors.New("workflow: no provider configured")
	}
	if job.ProviderJobID == "" {
		return StageResult{}, generation.Terminal("POLL_MISSING_PROVIDER_JOB_ID", "ProviderJobID is empty; cannot poll")
	}
	if result, ok, err := w.completedStagedWriteResult(ctx, job); ok || err != nil {
		return result, err
	}

	pname := providerName(w.Provider)
	pollReq := w.providerRequest(job, pname, obs.ProviderModePoll, hashProviderInput(job, pname), job.ProviderJobID)
	_ = w.putProviderRequest(ctx, pollReq)
	startPoll := w.now()
	status, err := w.Provider.PollAsync(ctx, job.ProviderJobID)
	w.emitProviderCall(ctx, pname, job.Model, obs.ProviderModePoll,
		float64(w.now().Sub(startPoll))/float64(time.Millisecond), err)
	if err != nil {
		_ = w.updateProviderRequest(ctx, job, pollReq.ID, ProviderRequestFailed, job.ProviderJobID, err)
		return StageResult{}, generation.Transient("POLL_ERROR", err.Error())
	}
	_ = w.updateProviderRequest(ctx, job, pollReq.ID, ProviderRequestSucceeded, job.ProviderJobID, nil)

	switch status {
	case generation.PollPending:
		// Re-enqueue to the same stage; SQS visibility-timeout back-off
		// handles the delay.
		return w.nextStageResult(ctx, job, generation.StageProviderWait, generation.ResourcePoll), nil

	case generation.PollReady:
		providerInputHash := hashProviderInput(job, pname)
		scope := stagedScopeFor(job.ID)
		claim, err := w.acquireStageClaim(ctx, scope, providerInputHash, "PROVIDER_FETCH")
		if err != nil {
			return StageResult{}, err
		}
		if claim.replayed {
			return w.providerSuccessResult(ctx, job)
		}
		token := claim.token

		fetchReq := w.providerRequest(job, pname, obs.ProviderModeFetch, providerInputHash, job.ProviderJobID)
		if err := w.putProviderRequest(ctx, fetchReq); err != nil {
			w.claimAbandon(ctx, scope, token)
			return StageResult{}, err
		}
		startFetch := w.now()
		art, ferr := w.Provider.FetchAsync(ctx, job.ProviderJobID)
		w.emitProviderCall(ctx, pname, job.Model, obs.ProviderModeFetch,
			float64(w.now().Sub(startFetch))/float64(time.Millisecond), ferr)
		if ferr != nil {
			_ = w.updateProviderRequest(ctx, job, fetchReq.ID, ProviderRequestFailed, job.ProviderJobID, ferr)
			terr := generation.Transient("FETCH_ERROR", ferr.Error())
			w.claimFailOrAbandon(ctx, scope, token, terr)
			return StageResult{}, terr
		}
		_ = w.updateProviderRequest(ctx, job, fetchReq.ID, ProviderRequestSucceeded, job.ProviderJobID, nil)
		result, err := w.stageArtifactAndCommit(ctx, job, art, token, scope, fetchReq.ID)
		if err == nil {
			result.ProviderRequestID = fetchReq.ID
		}
		return result, err

	case generation.PollFailed:
		result := StageResult{NextStage: StageTerminal, TerminalError: &generation.Error{
			Code:     "PROVIDER_JOB_FAILED",
			Message:  "async provider reported job failure",
			Terminal: true,
		}}
		if w.QuotaLedger != nil && job.BudgetDate != "" {
			op := w.QuotaLedger.LedgerUpdateReleased(job.TenantID, job.ID, job.BudgetDate, job.BudgetMicroUSD)
			result.LedgerOp = &op
		}
		return result, nil

	default:
		return StageResult{}, generation.Terminal("UNKNOWN_POLL_STATUS", string(status))
	}
}

// stageArtifactAndCommit writes the provider's raw output into the staging
// area, commits the budget (the provider was called, so the money is spent
// regardless of whether the gate passes downstream), and transitions to
// DISCLOSURE_POSTPROCESS. Notably, this function does NOT call
// VerifyPublishableArtifact — see workflow.go for why the gate runs after
// postprocess mutations rather than against raw provider bytes.
//
// token and scope identify the staged-write idempotency claim for sync
// inference and async fetch. Pass empty strings only for unguarded tests.
func (w *Workflow) stageArtifactAndCommit(ctx context.Context, job *generation.Job, art generation.Artifact, token, scope, serviceRequestID string) (StageResult, error) {
	if w.Stager == nil {
		return StageResult{}, errors.New("workflow: no staged artifact store")
	}
	ttl := w.StagedTTL
	if ttl <= 0 {
		ttl = 24 * time.Hour
	}
	ref, serr := w.Stager.PutStaged(ctx, *job, art, ttl)
	if serr != nil {
		w.claimAbandon(ctx, scope, token)
		return StageResult{}, fmt.Errorf("workflow: stage artifact: %w", serr)
	}

	if cerr := w.claimComplete(ctx, scope, token, ref.StorageKey); cerr != nil {
		// Best-effort: roll back the staged bytes so a retry sees a clean
		// slate. The S3 lifecycle rule is the long-stop GC.
		_ = w.Stager.DeleteStaged(ctx, ref)
		return StageResult{}, fmt.Errorf("workflow: complete claim: %w", cerr)
	}

	// The provider call already happened, so the budget is spent regardless
	// of what the platform safety gate decides next. Commit here and let
	// OUTPUT_MODERATION drive the publish/block decision against the
	// staged bytes.
	result, rerr := w.providerSuccessResult(ctx, job)
	if rerr != nil {
		return StageResult{}, rerr
	}
	if w.UsageMeter != nil {
		cost := job.BudgetMicroUSD
		if cost == 0 {
			cost = DefaultCostMicroUSD(job.OutputType)
		}
		_ = w.UsageMeter.RecordVendorCost(ctx, providerName(w.Provider), job.ID, cost)
		_ = w.UsageMeter.RecordServiceCost(ctx, job.ID, ServiceCostSourceProviderSubmit, serviceRequestID, cost)
	}
	return result, nil
}

// stagedScopeFor returns the idempotency scope used by INFER → staged-write.
// Distinct from postprocessScopeFor so a postprocess crash doesn't put the
// inference claim into an ambiguous state.
func stagedScopeFor(jobID string) string {
	return genScope(jobID, "STAGED_WRITE")
}

func asyncSubmitScopeFor(jobID string) string {
	return genScope(jobID, "ASYNC_SUBMIT")
}

func (w *Workflow) completedStagedWriteResult(ctx context.Context, job *generation.Job) (StageResult, bool, error) {
	if w.Idempotency == nil {
		return StageResult{}, false, nil
	}
	stagedKey, status, err := w.Idempotency.GetResult(ctx, stagedScopeFor(job.ID))
	if err != nil || status != idempotency.StatusCompleted || stagedKey == "" {
		return StageResult{}, false, nil
	}
	result, rerr := w.providerSuccessResult(ctx, job)
	return result, true, rerr
}

func (w *Workflow) providerSuccessResult(ctx context.Context, job *generation.Job) (StageResult, error) {
	result := w.nextStageResult(ctx, job, generation.StageOutputModeration, generation.ResourceFast)
	if w.QuotaLedger != nil {
		op := w.QuotaLedger.LedgerUpdateCommitted(job.TenantID, job.ID, job.BudgetDate, job.BudgetMicroUSD)
		result.LedgerOp = &op
		return result, nil
	}
	if w.QuotaReserver != nil {
		if cerr := w.QuotaReserver.Commit(ctx, job.TenantID, job.BudgetDate, DefaultCostMicroUSD(job.OutputType)); cerr != nil {
			return StageResult{}, generation.Terminal("BUDGET_COMMIT_FAILED", cerr.Error())
		}
	}
	return result, nil
}

func vendorRequestID(job *generation.Job, provider string) string {
	return "vr_" + idempotency.HashInputs(job.TenantID, job.ID, provider, job.PreparedPromptHash, job.GenerationParamsHash)[:24]
}

func (w *Workflow) providerRequest(job *generation.Job, provider, callType, requestHash, vendorRequestID string) ProviderRequest {
	id := "pr_" + idempotency.HashInputs(job.ID, provider, callType, requestHash, w.now().Format(time.RFC3339Nano))[:24]
	return ProviderRequest{
		ID:                    id,
		TenantID:              job.TenantID,
		JobID:                 job.ID,
		Provider:              provider,
		Model:                 job.Model,
		CallType:              callType,
		RequestHash:           requestHash,
		VendorRequestID:       vendorRequestID,
		VendorIdempotencyMode: genprovider.VendorIdempotency(w.Provider),
		Status:                ProviderRequestSubmitted,
		CreatedAt:             w.now(),
		UpdatedAt:             w.now(),
	}
}

func (w *Workflow) putProviderRequest(ctx context.Context, req ProviderRequest) error {
	if w.ProviderRequests == nil {
		return nil
	}
	return w.ProviderRequests.PutProviderRequest(ctx, req)
}

func (w *Workflow) updateProviderRequest(ctx context.Context, job *generation.Job, requestID string, status ProviderRequestStatus, providerJobID string, err error) error {
	if w.ProviderRequests == nil {
		return nil
	}
	return w.ProviderRequests.UpdateProviderRequest(ctx, job.TenantID, job.ID, requestID, status, providerJobID, err)
}
