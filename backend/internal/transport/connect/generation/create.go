package generation

import (
	"context"
	"errors"
	"strconv"
	"strings"

	connect "connectrpc.com/connect"

	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	generationpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/generation/v1"
)

func (s *Server) CreateGeneration(ctx context.Context, req *connect.Request[generationpb.CreateGenerationRequest]) (*connect.Response[generationpb.CreateGenerationResponse], error) {
	ctx = localOnlyGenerationContext(ctx)
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetPrompt()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("prompt required"))
	}
	if strings.TrimSpace(req.Msg.GetIdempotencyKey()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("idempotency_key required"))
	}
	spec, err := createGenerationSpec(req.Msg)
	if err != nil {
		return nil, err
	}
	out, err := s.submit(contextWithRequestTraceparent(ctx, req.Header().Get("traceparent")), claims, spec)
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(out), nil
}

func (s *Server) CreateAudioOverview(ctx context.Context, req *connect.Request[generationpb.CreateAudioOverviewRequest]) (*connect.Response[generationpb.CreateAudioOverviewResponse], error) {
	ctx = localOnlyGenerationContext(ctx)
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.Msg.GetTopic()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("topic required"))
	}
	if strings.TrimSpace(req.Msg.GetIdempotencyKey()) == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("idempotency_key required"))
	}
	out, err := s.submit(contextWithRequestTraceparent(ctx, req.Header().Get("traceparent")), claims, generationSubmitSpec{
		OutputType:     domaingen.OutputAudio,
		Prompt:         req.Msg.GetTopic(),
		Tier:           req.Msg.GetTier(),
		Model:          req.Msg.GetModel(),
		Provider:       strings.TrimSpace(req.Msg.GetProvider()),
		IdempotencyKey: req.Msg.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, err
	}
	return connect.NewResponse(&generationpb.CreateAudioOverviewResponse{Generation: out.Generation}), nil
}

type generationSubmitSpec struct {
	OutputType      domaingen.OutputType
	Prompt          string
	Tier            string
	Model           string
	Provider        string
	ResolutionLabel string
	Seed            int64
	IdempotencyKey  string
}

func createGenerationSpec(req *generationpb.CreateGenerationRequest) (generationSubmitSpec, error) {
	if req.GetOutputType() != generationpb.OutputType_OUTPUT_TYPE_UNSPECIFIED && req.GetOutputType() != generationpb.OutputType_OUTPUT_TYPE_IMAGE {
		return generationSubmitSpec{}, connect.NewError(connect.CodeInvalidArgument, errors.New("CreateGeneration supports image output only; use CreateAudioOverview for audio"))
	}
	resolution := ""
	if req.GetResolution() != "" {
		canonical, err := canonicalResolution(req.GetResolution())
		if err != nil {
			return generationSubmitSpec{}, err
		}
		resolution = canonical
	}
	seed := int64(0)
	if req.Seed != nil {
		seed = int64(req.GetSeed())
	}
	return generationSubmitSpec{
		OutputType:      domaingen.OutputImage,
		Prompt:          req.GetPrompt(),
		Tier:            req.GetTier(),
		Model:           req.GetModel(),
		Provider:        strings.TrimSpace(req.GetProvider()),
		ResolutionLabel: resolution,
		Seed:            seed,
		IdempotencyKey:  req.GetIdempotencyKey(),
	}, nil
}

func canonicalResolution(value string) (string, error) {
	w, h, ok := strings.Cut(strings.ToLower(strings.TrimSpace(value)), "x")
	if !ok || w == "" || h == "" {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("resolution must be WIDTHxHEIGHT"))
	}
	width, werr := strconv.Atoi(w)
	height, herr := strconv.Atoi(h)
	if werr != nil || herr != nil || width <= 0 || height <= 0 || width > 4096 || height > 4096 {
		return "", connect.NewError(connect.CodeInvalidArgument, errors.New("resolution must be between 1x1 and 4096x4096"))
	}
	return strconv.Itoa(width) + "x" + strconv.Itoa(height), nil
}
