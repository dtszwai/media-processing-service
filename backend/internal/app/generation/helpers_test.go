package generation_test

import (
	"testing"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/postprocess"
)

// newTestWorkflow builds a permissive Workflow for tests. Callers mutate the
// returned Workflow's exported fields when a test needs different wiring.
func newTestWorkflow(t *testing.T, repo gen.JobRepository, prov genprovider.Provider, idem idempotency.Store, sink gen.ArtifactSink) *gen.Workflow {
	t.Helper()
	wf, err := gen.NewWorkflow(gen.Workflow{
		Repo:         repo,
		Provider:     prov,
		Idempotency:  idem,
		ArtifactSink: sink,
		Stager:       gen.NewMemStaging(),
		ImageStamper: testStamper(t),
	})
	if err != nil {
		t.Fatalf("newTestWorkflow: %v", err)
	}
	return wf
}

// testStamper returns a working postprocess.Stamper for tests that exercise
// image output; the AI-disclosure gate requires a stamped fingerprint.
func testStamper(t *testing.T) *postprocess.Stamper {
	t.Helper()
	s, err := postprocess.NewStamper("AI Generated")
	if err != nil {
		t.Fatalf("test stamper: %v", err)
	}
	return s
}
