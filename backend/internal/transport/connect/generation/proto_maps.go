package generation

import (
	"time"

	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/pbutil"
	generationpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/generation/v1"
)

func jobToProto(j domaingen.Job) *generationpb.Generation {
	g := &generationpb.Generation{
		JobId:           j.ID,
		MediaId:         j.MediaID,
		Status:          string(j.Status),
		Stage:           protoutil.PtrString(string(j.CurrentStage)),
		CreatedAt:       protoutil.PtrString(j.CreatedAt.UTC().Format(time.RFC3339)),
		UpdatedAt:       protoutil.PtrString(j.UpdatedAt.UTC().Format(time.RFC3339)),
		IsAiGenerated:   protoutil.PtrBool(true),
		GenerationId:    protoutil.PtrString(generationIDFromJobID(j.ID)),
		TenantId:        protoutil.PtrString(j.TenantID),
		CreatedByUserId: protoutil.PtrString(j.UserID),
		OutputType:      outputTypeToProtoPtr(j.OutputType),
		Mode:            modeToProtoPtr(domaingen.GenerationModeCreate),
		ActiveJobId:     protoutil.PtrString(j.ID),
		PrimaryOutputId: protoutil.PtrString(outputIDFromJobID(j.ID)),
	}
	if j.Error != nil {
		g.ErrorCode = protoutil.PtrString(j.Error.Code)
		g.ErrorMessage = protoutil.PtrString(j.Error.Message)
	}
	return g
}

func identitySuffix(jobID string) string {
	if len(jobID) >= 4 && jobID[:4] == "gen_" {
		return jobID[4:]
	}
	return jobID
}

func generationIDFromJobID(jobID string) string { return "gen_" + identitySuffix(jobID) }

func outputIDFromJobID(jobID string) string { return "out_" + identitySuffix(jobID) }

func outputToProto(out domaingen.Output, variants []domaingen.Variant, urls map[string]string) *generationpb.Output {
	pb := &generationpb.Output{
		OutputId:              out.ID,
		TenantId:              out.TenantID,
		MediaId:               out.MediaID,
		GenerationId:          out.GenerationID,
		JobId:                 out.JobID,
		Type:                  outputTypeToProto(out.Type),
		Status:                string(out.Status),
		VariantCountRequested: int32(out.VariantCountRequested),
		VariantCountCompleted: int32(out.VariantCountCompleted),
		DefaultVariantId:      out.DefaultVariantID,
		CreatedAt:             protoutil.OptTimestamp(out.CreatedAt),
		UpdatedAt:             protoutil.OptTimestamp(out.UpdatedAt),
		CompletedAt:           protoutil.OptTimestamp(ptrTimeValue(out.CompletedAt)),
	}
	pb.Variants = variantsToProto(variants, urls)
	return pb
}

func variantsToProto(variants []domaingen.Variant, urls map[string]string) []*generationpb.Variant {
	out := make([]*generationpb.Variant, 0, len(variants))
	for _, v := range variants {
		pb := &generationpb.Variant{
			Url:                       urls[v.ID],
			Mime:                      v.MIME,
			Bytes:                     v.Bytes,
			VariantId:                 protoutil.PtrString(v.ID),
			OutputId:                  protoutil.PtrString(v.OutputID),
			GenerationId:              protoutil.PtrString(v.GenerationID),
			TenantId:                  protoutil.PtrString(v.TenantID),
			MediaId:                   protoutil.PtrString(v.MediaID),
			Index:                     protoutil.PtrInt32(int32(v.Index)),
			Status:                    protoutil.PtrString(string(v.Status)),
			FinalAssetId:              protoutil.PtrString(v.FinalAssetID),
			StagedArtifactId:          protoutil.OptString(v.StagedArtifactID),
			Provider:                  protoutil.OptString(v.Provider),
			Model:                     protoutil.OptString(v.Model),
			Seed:                      protoutil.OptString(v.Seed),
			ProviderRequestId:         protoutil.OptString(v.ProviderRequestID),
			SafetyCaseId:              protoutil.OptString(v.SafetyCaseID),
			ProvenanceManifestAssetId: protoutil.OptString(v.ProvenanceManifestAssetID),
			CompletedAt:               protoutil.OptTimestamp(ptrTimeValue(v.CompletedAt)),
			CreatedAt:                 protoutil.OptTimestamp(v.CreatedAt),
			UpdatedAt:                 protoutil.OptTimestamp(v.UpdatedAt),
		}
		if v.Score != nil {
			pb.Score = v.Score
		}
		if v.Error != nil {
			pb.ErrorCode = protoutil.OptString(v.Error.Code)
			pb.ErrorMessage = protoutil.OptString(v.Error.Message)
		}
		out = append(out, pb)
	}
	return out
}

func ptrTimeValue(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

func outputTypeToProtoPtr(t domaingen.OutputType) *generationpb.OutputType {
	v := outputTypeToProto(t)
	return &v
}

func outputTypeToProto(t domaingen.OutputType) generationpb.OutputType {
	switch t {
	case domaingen.OutputImage:
		return generationpb.OutputType_OUTPUT_TYPE_IMAGE
	case domaingen.OutputAudio:
		return generationpb.OutputType_OUTPUT_TYPE_AUDIO
	default:
		return generationpb.OutputType_OUTPUT_TYPE_UNSPECIFIED
	}
}

func modeToProtoPtr(mode domaingen.GenerationMode) *generationpb.Mode {
	v := generationpb.Mode_MODE_UNSPECIFIED
	switch mode {
	case domaingen.GenerationModeCreate:
		v = generationpb.Mode_MODE_CREATE
	case domaingen.GenerationModeEdit:
		v = generationpb.Mode_MODE_EDIT
	case domaingen.GenerationModeRerun:
		v = generationpb.Mode_MODE_RERUN
	}
	return &v
}
