package generation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"path"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/postprocess"
)

// stagePostprocess is the canonical home for byte-level mutations and the
// AI-disclosure gate. It re-loads the staged artifact, runs the mutation
// pipeline (watermark stamping for images; pass-through for other types),
// runs the gate, writes the final asset, and acks the inference claim.
//
// Replay model:
//   - First entry: claim DISCLOSURE_POSTPROCESS_FINAL, mutate+gate+sink, complete.
//   - Crash after claim completion: claim observes REPLAY_COMPLETED, returns
//     the cached asset id without re-mutating or re-charging.
//   - Crash after sink but before claim completion: retry re-runs sink against
//     deterministic final rows; the DDB sink accepts the same asset id as an
//     idempotent replay.
//   - Staged bytes expired: terminal STAGED_EXPIRED. The audit row records
//     the rejection. Re-running inference is intentionally an operator
//     decision rather than an automatic fallback — budget was already
//     committed at INFER and a silent re-charge would surprise tenants.
func (w *Workflow) stagePostprocess(ctx context.Context, job *generation.Job) (StageResult, error) {
	if w.Stager == nil {
		return StageResult{}, errors.New("workflow: no staged artifact store")
	}
	if w.ArtifactSink == nil {
		return StageResult{}, errors.New("workflow: no artifact sink")
	}

	scope := postprocessScopeFor(job.ID)
	claim, err := w.acquireStageClaim(ctx, scope, hashJobInput(job), "DISCLOSURE_POSTPROCESS")
	if err != nil {
		return StageResult{}, err
	}
	if claim.replayed {
		result := StageResult{Outcome: OutcomeDisclosureComplete}
		result.ResultAssetID = claim.replayResult
		return result, nil
	}
	token := claim.token

	ref, art, lerr := w.loadStagedForPostprocess(ctx, *job)
	if lerr != nil {
		w.claimFailOrAbandon(ctx, scope, token, lerr)
		return StageResult{}, lerr
	}

	mutated, merr := w.runPostprocessPipeline(ctx, *job, art)
	if merr != nil {
		w.claimAbandon(ctx, scope, token)
		return StageResult{}, merr
	}

	// Content-class boundary: the bytes must be servable as the declared
	// OutputType before the disclosure gate even sees them. Rejecting here
	// keeps generic binary off the canonical storage path entirely.
	if verr := ValidateProviderArtifact(ArtifactPolicyInput{
		JobID:      job.ID,
		Provider:   job.Provider,
		OutputType: job.OutputType,
		Stage:      job.CurrentStage,
		Artifact:   mutated,
	}); verr != nil {
		slog.ErrorContext(ctx, "provider artifact rejected by content-class policy",
			"job_id", job.ID,
			"provider", job.Provider,
			"output_type", string(job.OutputType),
			"content_type", mutated.ContentType,
			"extension", mutated.Extension,
			"stage", string(job.CurrentStage),
			"code", generation.AsError(verr).Code,
		)
		w.claimFail(ctx, scope, token, generation.AsError(verr).Code)
		return StageResult{}, verr
	}

	decision := buildGateDecision(job, mutated, w.now())
	if gerr := VerifyPublishableArtifact(mutated, job.OutputType); gerr != nil {
		decision.Decision = "FAIL"
		decision.ErrorCode = generation.AsError(gerr).Code
		w.claimFail(ctx, scope, token, decision.ErrorCode)
		return StageResult{}, gateRejectedError{cause: gerr, decision: decision}
	}
	decision.Decision = "PASS"

	assetID, sinkErr := w.ArtifactSink.StoreFinalArtifact(ctx, *job, mutated)
	if sinkErr != nil {
		w.claimAbandon(ctx, scope, token)
		return StageResult{}, fmt.Errorf("workflow: store artifact: %w", sinkErr)
	}
	if w.UsageMeter != nil {
		_ = w.UsageMeter.RecordGeneratedOutput(ctx, job.TenantID, job.ID, assetID)
	}

	if cerr := w.claimComplete(ctx, scope, token, assetID); cerr != nil {
		return StageResult{}, fmt.Errorf("workflow: complete postprocess claim: %w", cerr)
	}

	// Drop the staged bytes — final asset is in place and the staged copy is
	// no longer reachable as a recovery source. S3 lifecycle is the long-stop
	// GC if this fails.
	_ = w.Stager.DeleteStaged(ctx, ref)

	result := StageResult{Outcome: OutcomeDisclosureComplete}
	result.ResultAssetID = assetID
	result.GateDecision = &decision
	return result, nil
}

// loadStagedForPostprocess reconstructs the staged ref from the inference
// idempotency record and hydrates the artifact bytes. The idempotency result
// for stagedScopeFor(jobID) is the staging S3 key (set in
// stageArtifactAndCommit). Failure modes:
//   - Inference not yet completed: NO_STAGED_ARTIFACT (terminal — shouldn't
//     happen if the FSM dispatched DISCLOSURE_POSTPROCESS).
//   - Bytes missing in S3: STAGED_EXPIRED (terminal).
//   - Storage transient error: surfaced unmodified so SQS retries.
func (w *Workflow) loadStagedForPostprocess(ctx context.Context, job generation.Job) (StagedRef, generation.Artifact, error) {
	if w.Idempotency == nil {
		return StagedRef{}, generation.Artifact{}, generation.Terminal("NO_STAGED_ARTIFACT", "workflow has no idempotency store to locate staged artifact")
	}
	stagedKey, status, gerr := w.Idempotency.GetResult(ctx, stagedScopeFor(job.ID))
	if gerr != nil {
		return StagedRef{}, generation.Artifact{}, generation.Terminal("NO_STAGED_ARTIFACT", "inference claim not found: "+gerr.Error())
	}
	if status != idempotency.StatusCompleted || stagedKey == "" {
		return StagedRef{}, generation.Artifact{}, generation.Terminal("NO_STAGED_ARTIFACT", "inference claim is not COMPLETED")
	}
	// Build a minimal ref. Provider metadata + content type are restored when
	// we reach LoadStaged because the Stager owns the canonical record (DDB
	// row or in-memory map). For mem testing the in-memory map is keyed by
	// StorageKey alone; for DDB the impl re-reads the tracking row by
	// (TenantID, JobID), so they must be set here.
	ref := StagedRef{
		StorageKey: stagedKey,
		Extension:  extFromStorageKey(stagedKey),
		TenantID:   job.TenantID,
		JobID:      job.ID,
	}
	art, lerr := w.Stager.LoadStaged(ctx, ref)
	if lerr != nil {
		if errors.Is(lerr, ErrStagedNotFound) {
			return StagedRef{}, generation.Artifact{}, generation.Terminal("STAGED_EXPIRED", "staged artifact missing — likely past 24h TTL")
		}
		return StagedRef{}, generation.Artifact{}, generation.Transient("STAGED_LOAD_ERROR", lerr.Error())
	}
	return ref, art, nil
}

// runPostprocessPipeline is the mutation hook. For image artifacts it runs
// the watermark stamper which mutates the PNG bytes and stamps disclosure
// metadata the gate verifies. Non-image outputs pass through — audio/video
// disclosure work plugs into this same function as they need it.
//
// Skipping the stamper (ImageStamper == nil) is a permissive default for
// tests. Production wires a real stamper and the gate rejects any image
// artifact whose watermark.fingerprint field isn't a real hash.
func (w *Workflow) runPostprocessPipeline(_ context.Context, job generation.Job, art generation.Artifact) (generation.Artifact, error) {
	if job.OutputType != generation.OutputImage || w.ImageStamper == nil {
		return art, nil
	}
	stamped, meta, err := w.ImageStamper.StampImage(art.Bytes)
	if err != nil {
		return generation.Artifact{}, generation.Terminal("WATERMARK_STAMP_FAILED", err.Error())
	}
	merged := make(map[string]string, len(art.Metadata)+len(meta))
	maps.Copy(merged, art.Metadata)
	maps.Copy(merged, meta)
	// Stamper fingerprint doubles as the gate's `visible_watermark` field.
	merged["visible_watermark"] = meta[postprocess.MetadataKeys.Fingerprint]
	return generation.Artifact{
		Bytes:       stamped,
		ContentType: art.ContentType,
		Extension:   art.Extension,
		// Stamper already returned SHA-256(stamped) as the fingerprint —
		// reuse it instead of re-hashing the same bytes.
		SHA256:   meta[postprocess.MetadataKeys.Fingerprint],
		Metadata: merged,
	}, nil
}

// postprocessScopeFor returns the idempotency scope used by the DISCLOSURE_POSTPROCESS
// stage to gate gate-and-sink. Distinct from the inference scope so replays
// of either stage are independently observable.
func postprocessScopeFor(jobID string) string {
	return genScope(jobID, "DISCLOSURE_POSTPROCESS_FINAL")
}

type gateRejectedError struct {
	cause    error
	decision GateDecision
}

func (e gateRejectedError) Error() string { return e.cause.Error() }

func (e gateRejectedError) Unwrap() error { return e.cause }

func gateDecisionFromError(err error) (GateDecision, bool) {
	var gateErr gateRejectedError
	if errors.As(err, &gateErr) {
		return gateErr.decision, true
	}
	return GateDecision{}, false
}

// extFromStorageKey extracts the file extension from a staged S3 key.
// S3 keys use forward slashes so `path.Ext` (not `filepath.Ext`) is correct.
func extFromStorageKey(key string) string {
	if ext := path.Ext(key); ext != "" {
		return ext[1:]
	}
	return "bin"
}
