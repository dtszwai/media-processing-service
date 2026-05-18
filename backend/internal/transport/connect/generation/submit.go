package generation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	connect "connectrpc.com/connect"

	generationapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
	generationpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/generation/v1"
)

// submit is the thin transport adapter: validate, build the principal-aware
// SubmitCommand, hand it to the SubmissionService, and map the result and
// typed errors back to Connect codes.
func (s *Server) submit(ctx context.Context, claims *jwtauth.Claims, spec generationSubmitSpec) (*generationpb.CreateGenerationResponse, error) {
	if spec.Model == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("spec.model is required: client must pick a provider-specific model"))
	}
	res, err := s.submissions.Create(ctx, generationapp.SubmitCommand{
		TenantID:        claims.TenantID,
		UserID:          claims.Subject,
		Prompt:          spec.Prompt,
		Provider:        spec.Provider,
		Model:           spec.Model,
		OutputType:      spec.OutputType,
		Tier:            tierFromString(spec.Tier),
		ResolutionLabel: spec.ResolutionLabel,
		Seed:            spec.Seed,
		IdempotencyKey:  spec.IdempotencyKey,
	})
	if err != nil {
		return nil, mapSubmissionError(err)
	}
	return &generationpb.CreateGenerationResponse{Generation: jobToProto(res.Job)}, nil
}

func tierFromString(raw string) domaingen.Tier {
	if strings.EqualFold(raw, "paid") {
		return domaingen.TierPaid
	}
	return domaingen.TierFree
}

// mapSubmissionError maps app/generation sentinel errors to Connect codes.
// Unmatched errors fall through as CodeInternal so transport never silently
// downgrades a domain failure.
func mapSubmissionError(err error) error {
	switch {
	case errors.Is(err, generationapp.ErrIdempotencyKeyConflict):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, generationapp.ErrSubmitInFlight):
		return connect.NewError(connect.CodeAborted, err)
	case errors.Is(err, generationapp.ErrBudgetInsufficient):
		return connect.NewError(connect.CodeResourceExhausted, errors.New("BUDGET_INSUFFICIENT: tenant daily budget insufficient; job was not created"))
	case errors.Is(err, generationapp.ErrPriorSubmitFailed),
		errors.Is(err, generationapp.ErrSubmissionMalformed):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, generationapp.ErrSubmissionMisconfigured):
		return connect.NewError(connect.CodeInternal, err)
	default:
		return connect.NewError(connect.CodeInternal, fmt.Errorf("submit: %w", err))
	}
}

func contextWithRequestTraceparent(ctx context.Context, traceparent string) context.Context {
	return generationapp.ContextWithTraceparent(ctx, traceparent)
}
