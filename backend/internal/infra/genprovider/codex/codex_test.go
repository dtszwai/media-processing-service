package codex

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func TestInlineBytes(t *testing.T) {
	if !New().InlineBytes() {
		t.Fatal("codex provider must report InlineBytes=true")
	}
}

func TestGenerateSync_BinaryNotInPath_Terminal(t *testing.T) {
	p := New()
	p.Binary = "codex-does-not-exist-on-this-host"
	_, err := p.GenerateSync(context.Background(), generation.JobSpec{Prompt: "x"})
	if !generation.IsTerminal(err) {
		t.Fatalf("missing binary must be terminal: %v", err)
	}
}

func TestMaterializeImage_PrefersInlineBase64(t *testing.T) {
	want := []byte("\x89PNG\r\n\x1a\nstub")
	got, err := materializeImage(turnResult{ImageBase64: base64.StdEncoding.EncodeToString(want)})
	if err != nil {
		t.Fatalf("materializeImage: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("bytes mismatch: got %q, want %q", got, want)
	}
}

func TestMaterializeImage_FallsBackToSavedPath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.png")
	want := []byte("\x89PNG\r\n\x1a\nsaved")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := materializeImage(turnResult{ImageSavedPath: path})
	if err != nil {
		t.Fatalf("materializeImage: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("bytes mismatch: got %q, want %q", got, want)
	}
}

func TestMaterializeImage_NoImage_Transient(t *testing.T) {
	_, err := materializeImage(turnResult{})
	if err == nil {
		t.Fatal("expected error for empty result")
	}
	if generation.IsTerminal(err) {
		t.Fatalf("no-image error must be transient: %v", err)
	}
}
