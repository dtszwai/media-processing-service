//go:build integration

package generation_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	generation "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider/notebooklm"
)

// resolveBridge returns python/script/storage paths from the host
// $HOME/.notebooklm layout. Skips the test if any required artifact is missing.
func resolveBridge(t *testing.T) (python, script, state string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home dir: %v", err)
	}
	state = filepath.Join(home, ".notebooklm", "state.json")
	if _, err := os.Stat(state); err != nil {
		t.Skipf("notebooklm state.json not present at %s: %v", state, err)
	}
	venvPython := filepath.Join(home, ".notebooklm", "venv", "bin", "python3")
	if _, err := exec.LookPath(venvPython); err != nil {
		t.Skipf("notebooklm venv python not present at %s", venvPython)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Skipf("getwd: %v", err)
	}
	// repo root = ../../../..
	script = filepath.Join(cwd, "..", "..", "..", "..", "scripts", "notebooklm", "overview.py")
	if _, err := os.Stat(script); err != nil {
		t.Skipf("overview.py missing at %s", script)
	}
	return venvPython, script, state
}

func TestNotebookLM_Probe(t *testing.T) {
	if os.Getenv("TEST_INTEGRATION") != "1" {
		t.Skip("TEST_INTEGRATION not set")
	}
	python, script, state := resolveBridge(t)
	provider := notebooklm.New(python, script, state)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if err := provider.Probe(ctx); err != nil {
		t.Fatalf("Probe: %v", err)
	}
}

// TestNotebookLM_RealAudioOverview drives one real NotebookLM audio overview.
// This burns several minutes of wall time and uses the local NotebookLM browser
// session, so it requires TEST_NOTEBOOKLM_REAL=1 in addition to TEST_INTEGRATION=1.
func TestNotebookLM_RealAudioOverview(t *testing.T) {
	if os.Getenv("TEST_INTEGRATION") != "1" {
		t.Skip("TEST_INTEGRATION not set")
	}
	if os.Getenv("TEST_NOTEBOOKLM_REAL") != "1" {
		t.Skip("TEST_NOTEBOOKLM_REAL not set; skipping real audio overview generation")
	}
	python, script, state := resolveBridge(t)
	provider := notebooklm.New(python, script, state)
	provider.Timeout = 8 * time.Minute
	provider.AudioLength = "short"
	provider.AudioFormat = "brief"

	ctx, cancel := context.WithTimeout(context.Background(), 9*time.Minute)
	defer cancel()
	art, err := provider.GenerateSync(ctx, domaingen.JobSpec{
		JobID:           "gen_test_audio",
		MediaID:         "med_audio",
		TenantID:        "tenant-audio",
		OutputType:      domaingen.OutputAudio,
		Prompt:          "A short test about distributed systems retry policies.",
		Model:           "notebooklm",
		ClientRequestID: "test-audio-request",
	})
	if err != nil {
		t.Fatalf("GenerateSync: %v", err)
	}
	if len(art.Bytes) < 1024 {
		t.Fatalf("audio bytes suspiciously small: %d", len(art.Bytes))
	}
	if art.ContentType == "" || art.Extension == "" {
		t.Fatalf("missing content type/extension: %+v", art)
	}
	if art.SHA256 == "" {
		t.Fatalf("missing sha256")
	}
	if art.Metadata["disclosure"] != "AI_GENERATED_DISCLOSURE" {
		t.Fatalf("missing disclosure metadata: %+v", art.Metadata)
	}
	if err := generation.VerifyPublishableArtifact(art, domaingen.OutputAudio); err != nil {
		t.Fatalf("publish gate rejected audio: %v", err)
	}
}
