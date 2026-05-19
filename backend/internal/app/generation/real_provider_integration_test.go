//go:build integration

package generation_test

import (
	"context"
	"encoding/json"
	"fmt"
	"mime"
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

func TestSaveGeneratedReferenceArtifact(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TEST_GENERATED_ASSET_DIR", root)

	in := realProviderWorkflowInput{
		jobID:        "gen_reference_asset",
		mediaID:      "med_reference_asset",
		assetID:      "ast_reference_asset",
		providerName: "codex",
		model:        "gpt-test",
		outputType:   generation.OutputImage,
		prompt:       "test prompt",
	}
	artifact := generation.Artifact{
		Bytes:       []byte("image-bytes"),
		ContentType: "image/png",
		Extension:   "png",
		SHA256:      "sha256-reference",
		Metadata: map[string]string{
			genprovider.MetaProviderKey: "codex",
		},
	}
	saveGeneratedReferenceArtifact(t, in, artifact)

	dir := filepath.Join(root, "codex-image-gen_reference_asset")
	gotBytes, err := os.ReadFile(filepath.Join(dir, "artifact.png"))
	if err != nil {
		t.Fatalf("read artifact reference: %v", err)
	}
	if string(gotBytes) != "image-bytes" {
		t.Fatalf("artifact reference = %q, want image-bytes", string(gotBytes))
	}

	manifestBytes, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest struct {
		Artifact string `json:"artifact"`
		Bytes    int    `json:"bytes"`
		SHA256   string `json:"sha256"`
		Job      struct {
			ID       string `json:"id"`
			AssetID  string `json:"assetId"`
			Provider string `json:"provider"`
		} `json:"job"`
	}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}
	if manifest.Artifact != "artifact.png" || manifest.Bytes != len(artifact.Bytes) || manifest.SHA256 != artifact.SHA256 {
		t.Fatalf("manifest artifact summary = %+v, want artifact.png/%d/%s", manifest, len(artifact.Bytes), artifact.SHA256)
	}
	if manifest.Job.ID != in.jobID || manifest.Job.AssetID != in.assetID || manifest.Job.Provider != in.providerName {
		t.Fatalf("manifest job = %+v, want input job identity", manifest.Job)
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
	saveGeneratedReferenceArtifact(t, in, artifact)
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

func saveGeneratedReferenceArtifact(t *testing.T, in realProviderWorkflowInput, artifact generation.Artifact) {
	t.Helper()
	root := strings.TrimSpace(os.Getenv("TEST_GENERATED_ASSET_DIR"))
	if root == "" {
		return
	}

	dir := filepath.Join(root, safeArtifactPathPart(fmt.Sprintf("%s-%s-%s", in.providerName, in.outputType, in.jobID)))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create generated asset reference dir: %v", err)
	}

	assetName := "artifact." + artifactFileExtension(artifact)
	if err := os.WriteFile(filepath.Join(dir, assetName), artifact.Bytes, 0o644); err != nil {
		t.Fatalf("write generated asset reference: %v", err)
	}

	manifest := map[string]any{
		"generatedAt": time.Now().UTC().Format(time.RFC3339),
		"artifact":    assetName,
		"bytes":       len(artifact.Bytes),
		"sha256":      artifact.SHA256,
		"contentType": artifact.ContentType,
		"extension":   artifact.Extension,
		"metadata":    artifact.Metadata,
		"job": map[string]any{
			"id":         in.jobID,
			"mediaId":    in.mediaID,
			"assetId":    in.assetID,
			"provider":   in.providerName,
			"model":      in.model,
			"outputType": in.outputType,
			"prompt":     in.prompt,
		},
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal generated asset manifest: %v", err)
	}
	body = append(body, '\n')
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), body, 0o644); err != nil {
		t.Fatalf("write generated asset manifest: %v", err)
	}
	t.Logf("saved generated asset reference to %s", dir)
}

func artifactFileExtension(artifact generation.Artifact) string {
	ext := strings.Trim(strings.TrimSpace(artifact.Extension), ".")
	if ext != "" {
		return safeArtifactPathPart(ext)
	}
	if extensions, err := mime.ExtensionsByType(artifact.ContentType); err == nil && len(extensions) > 0 {
		return safeArtifactPathPart(strings.TrimPrefix(extensions[0], "."))
	}
	return "bin"
}

func safeArtifactPathPart(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var b strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-.")
	if out == "" {
		return "artifact"
	}
	return out
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
