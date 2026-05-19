package generation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/obs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type JobSubmitter interface {
	Submit(ctx context.Context, in SubmitInput) error
}

type IdempotencyGetter interface {
	GetResultWithHash(ctx context.Context, scope string) (ref string, inputHash string, status idempotency.Status, err error)
}

type BudgetCapacityHint interface {
	HasCapacity(ctx context.Context, tenantID, period string, requiredMicroUSD int64) (ok bool, availableMicroUSD int64, err error)
}

type AcceptedJobReader interface {
	GetJob(ctx context.Context, tenantID, jobID string) (*generation.Job, error)
}

var (
	ErrIdempotencyKeyConflict  = errors.New("submission: idempotency_key reused with different input")
	ErrSubmitInFlight          = errors.New("submission: already in flight")
	ErrPriorSubmitFailed       = errors.New("submission: prior submit failed")
	ErrSubmissionMalformed     = errors.New("submission: stored idempotency result malformed")
	ErrSubmissionMisconfigured = errors.New("submission: misconfigured")
	ErrBudgetInsufficient      = errors.New("submission: tenant daily budget insufficient")
)

type SubmissionService struct {
	Submitter    JobSubmitter
	ReplayReader AcceptedJobReader
	Idempotency  IdempotencyGetter
	CapacityHint BudgetCapacityHint
	Instruments  *obs.Instruments
	Now          func() time.Time
	NewID        func() string
}

type SubmitCommand struct {
	TenantID        string
	UserID          string
	Prompt          string
	Provider        string
	Model           string
	OutputType      generation.OutputType
	Tier            generation.Tier
	ResolutionLabel string
	Seed            int64
	IdempotencyKey  string
}

type SubmitResult struct {
	Job    generation.Job
	Replay bool
}

const submitVariantCount = 1

var noopSubmissionInstruments = obs.Noop()

func (s *SubmissionService) Create(ctx context.Context, cmd SubmitCommand) (SubmitResult, error) {
	if s == nil || s.Submitter == nil {
		return SubmitResult{}, fmt.Errorf("%w: no submitter configured", ErrSubmissionMisconfigured)
	}
	if s.NewID == nil {
		return SubmitResult{}, fmt.Errorf("%w: no id generator configured", ErrSubmissionMisconfigured)
	}
	now := s.now().UTC()

	scope := submitScope(cmd.TenantID, cmd.IdempotencyKey)
	inputHash := submitInputHash(cmd)

	if replayJob, replayed, err := s.replay(ctx, cmd, scope, inputHash, now); err != nil {
		return SubmitResult{}, err
	} else if replayed {
		return SubmitResult{Job: replayJob, Replay: true}, nil
	}

	if err := s.preflightBudget(ctx, cmd, now); err != nil {
		return SubmitResult{}, err
	}

	mediaID := "med_" + s.NewID()
	jobID := "gen_" + s.NewID()
	resultAssetID := "ast_" + s.NewID()

	job := generation.Job{
		ID:            jobID,
		TenantID:      cmd.TenantID,
		UserID:        cmd.UserID,
		MediaID:       mediaID,
		ResultAssetID: resultAssetID,
		OutputType:    cmd.OutputType,
		Tier:          cmd.Tier,
		Status:        generation.StatusQueued,
		CurrentStage:  generation.StageInputModeration,
		StageVersion:  1,
		Provider:      cmd.Provider,
		Prompt:        cmd.Prompt,
		Model:         cmd.Model,
		Resolution:    cmd.ResolutionLabel,
		Seed:          cmd.Seed,
		VariantCount:  submitVariantCount,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	m := media.Media{
		ID:              mediaID,
		TenantID:        cmd.TenantID,
		OwnerUserID:     cmd.UserID,
		Visibility:      media.DefaultVisibility(cmd.UserID),
		Origin:          media.OriginGenerated,
		Type:            mediaTypeForOutput(cmd.OutputType),
		Lifecycle:       media.LifecycleRunning,
		OriginalAssetID: resultAssetID,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	a := media.Asset{
		ID:        resultAssetID,
		MediaID:   mediaID,
		TenantID:  cmd.TenantID,
		Kind:      media.AssetKindGenerated,
		Role:      media.AssetRoleFinal,
		Operation: media.AssetOperationGenerationFinal,
		Lifecycle: media.AssetLifecyclePending,
		CreatedAt: now,
		UpdatedAt: now,
	}

	firstStageBody, ferr := MarshalStageMessage(cmd.TenantID, jobID, generation.StageInputModeration, 1, generation.ResourceFast, TraceparentFromContext(ctx))
	if ferr != nil {
		return SubmitResult{}, fmt.Errorf("submission: marshal first stage: %w", ferr)
	}

	if serr := s.Submitter.Submit(ctx, SubmitInput{
		Job:              job,
		Media:            m,
		ResultAsset:      a,
		IdempotencyScope: scope,
		InputHash:        inputHash,
		FirstStageBody:   firstStageBody,
	}); serr != nil {
		// A concurrent submitter may have won the claim race; re-check the
		// idempotency store so a winning peer's allocation surfaces as a
		// replay instead of a generic submit error.
		if replayJob, replayed, rerr := s.replay(ctx, cmd, scope, inputHash, now); rerr != nil {
			return SubmitResult{}, rerr
		} else if replayed {
			return SubmitResult{Job: replayJob, Replay: true}, nil
		}
		return SubmitResult{}, fmt.Errorf("submission: submit transaction: %w", serr)
	}
	return SubmitResult{Job: job}, nil
}

func (s *SubmissionService) replay(ctx context.Context, cmd SubmitCommand, scope, inputHash string, now time.Time) (generation.Job, bool, error) {
	if s.Idempotency == nil {
		return generation.Job{}, false, nil
	}
	ref, gotHash, st, err := s.Idempotency.GetResultWithHash(ctx, scope)
	if err != nil {
		// Storage transient errors fall through to the submit path; a real
		// conflict will surface there via Submit's conditional write.
		return generation.Job{}, false, nil
	}
	if gotHash != "" && gotHash != inputHash {
		return generation.Job{}, false, ErrIdempotencyKeyConflict
	}
	switch st {
	case idempotency.StatusCompleted:
		parts := strings.SplitN(ref, ":", 2)
		if len(parts) != 2 {
			return generation.Job{}, false, ErrSubmissionMalformed
		}
		if s.ReplayReader != nil {
			job, err := s.ReplayReader.GetJob(ctx, cmd.TenantID, parts[0])
			if err != nil {
				return generation.Job{}, false, fmt.Errorf("%w: replay job %s: %v", ErrSubmissionMalformed, parts[0], err)
			}
			return *job, true, nil
		}
		return generation.Job{
			ID:           parts[0],
			TenantID:     cmd.TenantID,
			MediaID:      parts[1],
			OutputType:   cmd.OutputType,
			Tier:         cmd.Tier,
			Status:       generation.StatusQueued,
			CurrentStage: generation.StageInputModeration,
			StageVersion: 1,
			CreatedAt:    now,
			UpdatedAt:    now,
		}, true, nil
	case idempotency.StatusClaimed:
		return generation.Job{}, false, ErrSubmitInFlight
	case idempotency.StatusFailed:
		return generation.Job{}, false, ErrPriorSubmitFailed
	}
	return generation.Job{}, false, nil
}

func (s *SubmissionService) preflightBudget(ctx context.Context, cmd SubmitCommand, now time.Time) error {
	if s.CapacityHint == nil {
		return nil
	}
	required := RequiredBudgetMicroUSD(BudgetEstimateFromSubmit(cmd))
	period := quota.PeriodDaily(now.UTC())
	ok, available, err := s.CapacityHint.HasCapacity(ctx, cmd.TenantID, period, required)
	if err != nil {
		s.emitBudgetPreflight(ctx, "error_fail_open", cmd)
		slog.WarnContext(ctx, "generation budget preflight failed open",
			"tenant_id", cmd.TenantID,
			"output_type", string(cmd.OutputType),
			"tier", string(cmd.Tier),
			"required_micro_usd", required,
			"err", err.Error(),
		)
		return nil
	}
	if !ok {
		s.emitBudgetPreflight(ctx, "rejected", cmd)
		s.emitSubmitRejected(ctx, "BUDGET_INSUFFICIENT", cmd)
		slog.InfoContext(ctx, "generation submit rejected by budget preflight",
			"tenant_id", cmd.TenantID,
			"output_type", string(cmd.OutputType),
			"tier", string(cmd.Tier),
			"required_micro_usd", required,
			"available_micro_usd", available,
		)
		return ErrBudgetInsufficient
	}
	s.emitBudgetPreflight(ctx, "passed", cmd)
	return nil
}

func (s *SubmissionService) emitBudgetPreflight(ctx context.Context, outcome string, cmd SubmitCommand) {
	s.instruments().BudgetPreflight.Add(ctx, 1, metric.WithAttributes(
		attribute.String("outcome", outcome),
		attribute.String("output_type", string(cmd.OutputType)),
		attribute.String("tier", string(cmd.Tier)),
	))
}

func (s *SubmissionService) emitSubmitRejected(ctx context.Context, reason string, cmd SubmitCommand) {
	s.instruments().SubmitRejected.Add(ctx, 1, metric.WithAttributes(
		attribute.String("reason", reason),
		attribute.String("output_type", string(cmd.OutputType)),
		attribute.String("tier", string(cmd.Tier)),
	))
}

func (s *SubmissionService) now() time.Time {
	if s.Now == nil {
		return time.Now().UTC()
	}
	return s.Now()
}

func (s *SubmissionService) instruments() *obs.Instruments {
	if s.Instruments != nil {
		return s.Instruments
	}
	return noopSubmissionInstruments
}

func submitScope(tenantID, idempotencyKey string) string {
	return "SUBMIT#" + tenantID + "#" + idempotencyKey
}

func submitInputHash(cmd SubmitCommand) string {
	return idempotency.HashInputs(
		cmd.TenantID,
		cmd.Prompt,
		cmd.Model,
		string(cmd.OutputType),
		cmd.Provider,
		string(cmd.Tier),
		cmd.ResolutionLabel,
		strconv.FormatInt(cmd.Seed, 10),
		strconv.Itoa(submitVariantCount),
	)
}

func mediaTypeForOutput(o generation.OutputType) media.Type {
	switch o {
	case generation.OutputImage:
		return media.TypeImage
	case generation.OutputAudio:
		return media.TypeAudio
	}
	return ""
}
