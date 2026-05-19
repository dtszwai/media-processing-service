package generation_test

import (
	"strings"
	"testing"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/simulated"
)

// ValidateProduction must surface a missing Stager (any output type) and a
// missing ImageStamper (image outputs) so production wiring bugs fail at
// first dispatch rather than at gate-rejection time.
func TestValidateProduction_FlagsMissingStagerAndImageStamper(t *testing.T) {
	wf, err := gen.NewWorkflow(gen.Workflow{
		Repo:     gen.NewMemRepo(),
		Provider: simulated.New(),
	})
	if err != nil {
		t.Fatalf("NewWorkflow: %v", err)
	}
	err = wf.ValidateProduction(generation.OutputImage)
	if err == nil {
		t.Fatalf("ValidateProduction(OutputImage) returned nil, want missing-deps error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "Stager") {
		t.Errorf("ValidateProduction error %q missing 'Stager'", msg)
	}
	if !strings.Contains(msg, "ImageStamper") {
		t.Errorf("ValidateProduction error %q missing 'ImageStamper'", msg)
	}
}
