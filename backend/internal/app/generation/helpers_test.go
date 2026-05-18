package generation_test

import (
	"testing"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
)

// newTestWorkflow builds a permissive Workflow for tests. Tests that need
// extra wiring mutate the returned Workflow's exported fields after the
// helper returns (e.g. wf.Stager, wf.QuotaReserver).
func newTestWorkflow(t *testing.T, repo gen.JobRepository, prov genprovider.Provider, idem idempotency.Store, sink gen.ArtifactSink) *gen.Workflow {
	t.Helper()
	wf, err := gen.NewWorkflow(gen.Workflow{
		Repo:         repo,
		Provider:     prov,
		Idempotency:  idem,
		ArtifactSink: sink,
	})
	if err != nil {
		t.Fatalf("newTestWorkflow: %v", err)
	}
	return wf
}
