package generation

import (
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// BudgetEstimateInput is the pricing-policy input shared by submit-time
// capacity hints and the authoritative COST_RESERVE stage. The first policy is
// still flat by output type, but the shape keeps future model/resolution/tier
// pricing changes in one helper.
type BudgetEstimateInput struct {
	OutputType   generation.OutputType
	Provider     string
	Model        string
	Resolution   string
	VariantCount int
	Tier         generation.Tier
}

func BudgetEstimateFromSubmit(cmd SubmitCommand) BudgetEstimateInput {
	return BudgetEstimateInput{
		OutputType:   cmd.OutputType,
		Provider:     cmd.Provider,
		Model:        cmd.Model,
		Resolution:   cmd.ResolutionLabel,
		VariantCount: submitVariantCount,
		Tier:         cmd.Tier,
	}
}

func BudgetEstimateFromJob(job generation.Job) BudgetEstimateInput {
	return BudgetEstimateInput{
		OutputType:   job.OutputType,
		Provider:     job.Provider,
		Model:        job.Model,
		Resolution:   job.Resolution,
		VariantCount: job.VariantCount,
		Tier:         job.Tier,
	}
}

func RequiredBudgetMicroUSD(in BudgetEstimateInput) int64 {
	switch in.OutputType {
	case generation.OutputImage:
		return 4_000 // $0.004
	case generation.OutputAudio:
		return 25_000 // $0.025
	default:
		return 1_000
	}
}

// DefaultCostMicroUSD is kept as the narrow output-type wrapper for tests and
// callers that do not yet have a full estimate shape.
func DefaultCostMicroUSD(outputType generation.OutputType) int64 {
	return RequiredBudgetMicroUSD(BudgetEstimateInput{OutputType: outputType})
}
