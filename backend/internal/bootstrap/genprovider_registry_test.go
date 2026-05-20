package bootstrap

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
)

func TestProviderRegistry_PickForJob(t *testing.T) {
	reg, err := NewProviderRegistry(app.GenerationConfig{})
	if err != nil {
		t.Fatalf("NewProviderRegistry: %v", err)
	}
	if p, err := reg.PickForJob(generation.OutputImage, "simulated"); err != nil || providerNameForTest(p) != "simulated" {
		t.Fatalf("named PickForJob: provider=%q err=%v", providerNameForTest(p), err)
	}
	if _, err := reg.PickForJob(generation.OutputImage, ""); generation.AsError(err).Code != "PROVIDER_UNAVAILABLE" {
		t.Fatalf("empty provider err = %v, want PROVIDER_UNAVAILABLE", err)
	}
	if _, err := reg.PickForJob(generation.OutputImage, "missing"); generation.AsError(err).Code != "PROVIDER_UNAVAILABLE" {
		t.Fatalf("missing provider err = %v, want PROVIDER_UNAVAILABLE", err)
	}
}

func TestProviderRegistry_CodexUnavailableWhenCLIMissing(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	reg, err := NewProviderRegistry(app.GenerationConfig{})
	if err != nil {
		t.Fatalf("registry construction should not fail on host-unavailable provider: %v", err)
	}
	if _, err := reg.PickForJob(generation.OutputImage, "codex"); generation.AsError(err).Code != "PROVIDER_UNAVAILABLE" {
		t.Fatalf("codex without CLI err = %v, want PROVIDER_UNAVAILABLE", err)
	}
}

func TestProviderRegistry_CodexConstructedWhenCLIPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	reg, err := NewProviderRegistry(app.GenerationConfig{})
	if err != nil {
		t.Fatalf("codex with stub binary: %v", err)
	}
	if p, err := reg.PickForJob(generation.OutputImage, "codex"); err != nil || providerNameForTest(p) != "codex" {
		t.Fatalf("PickForJob: provider=%q err=%v", providerNameForTest(p), err)
	}
}

func providerNameForTest(p genprovider.Provider) string {
	if p == nil {
		return ""
	}
	if named, ok := p.(genprovider.Named); ok {
		return named.Name()
	}
	return "unknown"
}
