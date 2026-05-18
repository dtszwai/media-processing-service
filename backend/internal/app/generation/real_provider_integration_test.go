//go:build integration

package generation_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
)

func TestRealProviderE2E_CodexImageWorkflow(t *testing.T) {
	requireRealProviderGate(t, "TEST_CODEX_REAL")
	registry := realProviderRegistry(t)
	provider, err := registry.PickForJob(generation.OutputImage, "codex")
	if err != nil {
		t.Fatalf("codex provider unavailable: %v. Ensure `codex` is on PATH and run `codex auth login` on this host.", err)
	}

	artifact := runRealProviderWorkflow(t, provider, realProviderWorkflowInput{
		jobID:           "gen_real_codex_image",
		mediaID:         "med_real_codex_image",
		assetID:         "ast_real_codex_image",
		providerName:    "codex",
		model:           "gpt-5.5",
		outputType:      generation.OutputImage,
		prompt:          "Create one clean test image of a small blue ceramic cup on a white table. No text, no logo, no watermark.",
		timeout:         15 * time.Minute,
		minBytes:        1024,
		contentTypePref: "image/",
		extension:       "png",
	})
	if artifact.Metadata[genprovider.MetaProviderKey] != "codex" {
		t.Fatalf("provider metadata = %q, want codex", artifact.Metadata[genprovider.MetaProviderKey])
	}
}

func TestRealProviderE2E_NotebookLMAudioWorkflow(t *testing.T) {
	requireRealProviderGate(t, "TEST_NOTEBOOKLM_REAL")
	registry := realProviderRegistry(t)
	provider, err := registry.PickForJob(generation.OutputAudio, "notebooklm")
	if err != nil {
		t.Fatalf("notebooklm provider unavailable: %v. Run `make notebooklm-import` first and set NOTEBOOKLM_* paths if you are not using the defaults.", err)
	}

	artifact := runRealProviderWorkflow(t, provider, realProviderWorkflowInput{
		jobID:           "gen_real_notebooklm_audio",
		mediaID:         "med_real_notebooklm_audio",
		assetID:         "ast_real_notebooklm_audio",
		providerName:    "notebooklm",
		model:           "notebooklm-default",
		outputType:      generation.OutputAudio,
		prompt:          "Create a short audio overview explaining why idempotent retries prevent duplicate billing in distributed job pipelines.",
		timeout:         12 * time.Minute,
		minBytes:        1024,
		contentTypePref: "audio/",
		extension:       "",
	})
	if artifact.Metadata[genprovider.MetaProviderKey] != "notebooklm" {
		t.Fatalf("provider metadata = %q, want notebooklm", artifact.Metadata[genprovider.MetaProviderKey])
	}
}

type realProviderWorkflowInput struct {
	jobID           string
	mediaID         string
	assetID         string
	providerName    string
	model           string
	outputType      generation.OutputType
	prompt          string
	timeout         time.Duration
	minBytes        int
	contentTypePref string
	extension       string
}

func runRealProviderWorkflow(t *testing.T, provider genprovider.Provider, in realProviderWorkflowInput) generation.Artifact {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), in.timeout)
	defer cancel()

	repo := gen.NewMemRepo()
	sink := gen.NewMemSink()
	sink.NextAssetID = in.assetID
	wf := newTestWorkflow(t, repo, provider, gen.NewMemIdempotency(), sink)
	now := time.Now().UTC()
	job := generation.Job{
		ID:            in.jobID,
		TenantID:      "tenant-real-provider",
		MediaID:       in.mediaID,
		ResultAssetID: in.assetID,
		OutputType:    in.outputType,
		Tier:          generation.TierPaid,
		Status:        generation.StatusRunning,
		CurrentStage:  generation.StageInputModeration,
		StageVersion:  1,
		Provider:      in.providerName,
		Prompt:        in.prompt,
		Model:         in.model,
		VariantCount:  1,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if err := repo.CreateJob(ctx, job); err != nil {
		t.Fatalf("CreateJob: %v", err)
	}
	if err := wf.Run(ctx, job.ID); err != nil {
		t.Fatalf("real provider workflow failed: %v", err)
	}
	got, err := repo.GetJob(ctx, job.TenantID, job.ID)
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	if got.Status != generation.StatusComplete || got.CurrentStage != generation.StageTerminal {
		t.Fatalf("final job state = status:%s stage:%s, want COMPLETE/TERMINAL", got.Status, got.CurrentStage)
	}
	if got.ResultAssetID != in.assetID {
		t.Fatalf("result asset id = %q, want %q", got.ResultAssetID, in.assetID)
	}
	if len(sink.Stored) != 1 {
		t.Fatalf("stored artifacts = %d, want 1", len(sink.Stored))
	}
	artifact := sink.Stored[0]
	if len(artifact.Bytes) < in.minBytes {
		t.Fatalf("artifact bytes = %d, want at least %d", len(artifact.Bytes), in.minBytes)
	}
	if !strings.HasPrefix(artifact.ContentType, in.contentTypePref) {
		t.Fatalf("content type = %q, want prefix %q", artifact.ContentType, in.contentTypePref)
	}
	if in.extension != "" && artifact.Extension != in.extension {
		t.Fatalf("extension = %q, want %q", artifact.Extension, in.extension)
	}
	if artifact.SHA256 == "" {
		t.Fatal("artifact sha256 is empty")
	}
	if err := gen.VerifyPublishableArtifact(artifact, in.outputType); err != nil {
		t.Fatalf("publish gate rejected real provider artifact: %v", err)
	}
	return artifact
}

func requireRealProviderGate(t *testing.T, providerGate string) {
	t.Helper()
	if os.Getenv("TEST_INTEGRATION") != "1" {
		t.Skip("TEST_INTEGRATION not set")
	}
	if os.Getenv(providerGate) != "1" {
		t.Skip(providerGate + " not set")
	}
}

func realProviderRegistry(t *testing.T) bootstrap.ProviderRegistry {
	t.Helper()
	registry, err := bootstrap.NewProviderRegistry(app.GenerationConfig{
		NotebookLM: app.NotebookLMConfig{
			ScriptPath:       envOr("NOTEBOOKLM_SCRIPT_PATH", filepath.Join(repoRoot(t), "scripts", "notebooklm", "overview.py")),
			StatePath:        envOr("NOTEBOOKLM_STORAGE_STATE_PATH", filepath.Join(homeDir(t), ".notebooklm", "state.json")),
			StateDisplayPath: os.Getenv("NOTEBOOKLM_STORAGE_STATE_DISPLAY_PATH"),
			PythonBin:        envOr("NOTEBOOKLM_PYTHON", filepath.Join(homeDir(t), ".notebooklm", "venv", "bin", "python3")),
		},
	})
	if err != nil {
		t.Fatalf("NewProviderRegistry: %v", err)
	}
	return registry
}

func repoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	return filepath.Clean(filepath.Join(cwd, "..", "..", "..", ".."))
}

func homeDir(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("UserHomeDir: %v", err)
	}
	return home
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
