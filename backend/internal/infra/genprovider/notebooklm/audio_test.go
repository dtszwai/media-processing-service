package notebooklm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func TestGenerateSync_UsesStructuredScriptError(t *testing.T) {
	cases := []struct {
		code     string
		terminal bool
	}{
		{code: "STATE_CORRUPT", terminal: true},
		{code: "AUTH_EXPIRED", terminal: true},
		{code: "RPC_TRANSIENT", terminal: false},
		{code: "RPC_PARSE_ERROR", terminal: true},
		{code: "GENERATION_FAILED", terminal: true},
		{code: "CONFIG_ERROR", terminal: true},
	}

	for _, tc := range cases {
		t.Run(tc.code, func(t *testing.T) {
			message := "message for " + tc.code
			provider := fakeScriptProvider(t, scriptFailure{
				stderrLines: []string{
					"notebooklm warning before structured failure",
					mustJSON(t, map[string]any{
						"code":     tc.code,
						"terminal": tc.terminal,
						"message":  message,
					}),
				},
				exitCode: 2,
			})

			_, err := provider.GenerateSync(context.Background(), generation.JobSpec{
				OutputType: generation.OutputAudio,
				Prompt:     "prompt",
			})
			var genErr *generation.Error
			if !errors.As(err, &genErr) {
				t.Fatalf("expected generation error, got %T: %v", err, err)
			}
			if genErr.Code != tc.code {
				t.Fatalf("code = %q, want %q", genErr.Code, tc.code)
			}
			if genErr.Terminal != tc.terminal {
				t.Fatalf("terminal = %v, want %v", genErr.Terminal, tc.terminal)
			}
			if genErr.Message != message {
				t.Fatalf("message = %q, want %q", genErr.Message, message)
			}
		})
	}
}

func TestGenerateSync_FallsBackToExitCodeWithoutStructuredScriptError(t *testing.T) {
	provider := fakeScriptProvider(t, scriptFailure{
		stderrLines: []string{"notebooklm error: old raw stderr"},
		exitCode:    2,
	})

	_, err := provider.GenerateSync(context.Background(), generation.JobSpec{
		OutputType: generation.OutputAudio,
		Prompt:     "prompt",
	})
	var genErr *generation.Error
	if !errors.As(err, &genErr) {
		t.Fatalf("expected generation error, got %T: %v", err, err)
	}
	if genErr.Code != "NOTEBOOKLM_RPC_FAILURE" {
		t.Fatalf("code = %q, want NOTEBOOKLM_RPC_FAILURE", genErr.Code)
	}
	if genErr.Terminal {
		t.Fatal("fallback exit=2 must stay transient")
	}
	if !strings.Contains(genErr.Message, "old raw stderr") {
		t.Fatalf("fallback message missing stderr: %q", genErr.Message)
	}
}

type scriptFailure struct {
	stderrLines []string
	exitCode    int
}

func fakeScriptProvider(t *testing.T, failure scriptFailure) *Provider {
	t.Helper()

	dir := t.TempDir()
	statePath := filepath.Join(dir, "state.json")
	if err := os.WriteFile(statePath, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Fatal(err)
	}

	scriptPath := filepath.Join(dir, "fake-overview.sh")
	var script strings.Builder
	for _, line := range failure.stderrLines {
		script.WriteString("cat >&2 <<'EOF'\n")
		script.WriteString(line)
		script.WriteString("\nEOF\n")
	}
	script.WriteString(fmt.Sprintf("exit %d\n", failure.exitCode))
	if err := os.WriteFile(scriptPath, []byte(script.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	provider := New(sh, scriptPath, statePath)
	provider.Timeout = 0
	return provider
}

func mustJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
