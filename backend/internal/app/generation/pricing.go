package generation

import (
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// DefaultCostMicroUSD returns the flat per-output-type cost estimate used for
// budget reservation. Stage-accurate cost models supersede this when wired.
func DefaultCostMicroUSD(outputType generation.OutputType) int64 {
	switch outputType {
	case generation.OutputImage:
		return 4_000 // $0.004
	case generation.OutputAudio:
		return 25_000 // $0.025
	default:
		return 1_000
	}
}
