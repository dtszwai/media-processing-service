package generation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	connect "connectrpc.com/connect"

	analyticsapp "github.com/dtszwai/media-processing-service/backend/internal/app/analytics"
	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/pbutil"
	generationpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/generation/v1"
)

func (s *Server) GetGeneration(ctx context.Context, req *connect.Request[generationpb.GetGenerationRequest]) (*connect.Response[generationpb.GetGenerationResponse], error) {
	ctx = localOnlyGenerationContext(ctx)
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("no repo"))
	}
	jobID, err := resolveReadJobID(req.Msg.GetJobId(), req.Msg.GetGenerationId())
	if err != nil {
		return nil, err
	}
	job, err := s.repo.GetJob(ctx, claims.TenantID, jobID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("job not found"))
	}
	if err := authorizeGenerationRead(claims, job); err != nil {
		return nil, err
	}
	resp := &generationpb.GetGenerationResponse{Generation: jobToProto(*job)}
	if s.outputReader != nil {
		output, variants, rerr := s.outputReader.GetOutputRollup(ctx, claims.TenantID, job.ID)
		if rerr == nil && output != nil {
			resp.Output = outputToProto(*output, variants, nil)
			resp.Variants = variantsToProto(variants, nil)
		}
	}
	return connect.NewResponse(resp), nil
}

func (s *Server) GetGenerationResult(ctx context.Context, req *connect.Request[generationpb.GetGenerationResultRequest]) (*connect.Response[generationpb.GetGenerationResultResponse], error) {
	ctx = localOnlyGenerationContext(ctx)
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	if s.repo == nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("no repo"))
	}
	jobID, err := resolveReadJobID(req.Msg.GetJobId(), req.Msg.GetGenerationId())
	if err != nil {
		return nil, err
	}
	job, err := s.repo.GetJob(ctx, claims.TenantID, jobID)
	if err != nil {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("job not found"))
	}
	if err := authorizeGenerationRead(claims, job); err != nil {
		return nil, err
	}
	if job.Status != domaingen.StatusComplete {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("job not yet complete (status=%s)", job.Status))
	}
	_, variants := s.readOutputRollup(ctx, claims.TenantID, job.ID)
	if req.Msg.GetVariantId() != "" {
		variants = filterVariant(variants, req.Msg.GetVariantId())
		if len(variants) == 0 {
			return nil, connect.NewError(connect.CodeNotFound, errors.New("variant not found"))
		}
	}
	resultAssetID := job.ResultAssetID
	if len(variants) > 0 && variants[0].FinalAssetID != "" {
		resultAssetID = variants[0].FinalAssetID
	}
	if resultAssetID == "" {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("result asset not yet allocated"))
	}
	url := ""
	var expiresAt time.Time
	if s.presigner != nil {
		u, exp, perr := s.presigner.PresignResult(ctx, claims.TenantID, job.MediaID, resultAssetID)
		if perr != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("presign result: %w", perr))
		}
		url = u
		expiresAt = exp
	}
	s.emitAnalytics(ctx, analyticsapp.Event{
		EventType:   analyticsapp.EventTypeGenerationResultFetch,
		TenantID:    claims.TenantID,
		MediaID:     job.MediaID,
		AssetID:     resultAssetID,
		PrincipalID: claims.Subject,
	})
	resp := &generationpb.GenerationResult{
		JobId:         job.ID,
		MediaId:       job.MediaID,
		Status:        string(job.Status),
		IsAiGenerated: protoutil.PtrBool(true),
	}
	switch job.OutputType {
	case domaingen.OutputImage:
		resp.ImageUrl = protoutil.PtrString(url)
	case domaingen.OutputAudio:
		resp.AudioUrl = protoutil.PtrString(url)
	}
	if !expiresAt.IsZero() {
		resp.ExpiresAt = protoutil.PtrString(expiresAt.UTC().Format(time.RFC3339))
	}
	if len(variants) > 0 {
		urls := map[string]string{}
		for _, v := range variants {
			if v.FinalAssetID == resultAssetID {
				urls[v.ID] = url
			}
		}
		resp.Variants = variantsToProto(variants, urls)
	}
	return connect.NewResponse(&generationpb.GetGenerationResultResponse{Result: resp}), nil
}

func (s *Server) readOutputRollup(ctx context.Context, tenantID, jobID string) (*domaingen.Output, []domaingen.Variant) {
	if s.outputReader == nil {
		return nil, nil
	}
	output, variants, err := s.outputReader.GetOutputRollup(ctx, tenantID, jobID)
	if err != nil {
		return nil, nil
	}
	return output, variants
}

func filterVariant(variants []domaingen.Variant, variantID string) []domaingen.Variant {
	for _, variant := range variants {
		if variant.ID == variantID {
			return []domaingen.Variant{variant}
		}
	}
	return nil
}

func resolveReadJobID(jobID, generationID string) (string, error) {
	jobID = strings.TrimSpace(jobID)
	generationID = strings.TrimSpace(generationID)
	if jobID == "" && generationID == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("job_id or generation_id required"))
	}
	if generationID == "" {
		return jobID, nil
	}
	if !strings.HasPrefix(generationID, "gen_") {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("generation_id must start with gen_"))
	}
	if jobID == "" {
		return generationID, nil
	}
	if generationIDFromJobID(jobID) != generationID {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("job_id and generation_id do not refer to the same generation"))
	}
	return jobID, nil
}
