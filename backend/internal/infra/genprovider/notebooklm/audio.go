// Package notebooklm wraps the scripts/notebooklm/overview.py
// Playwright/notebooklm-py bridge as a Go subprocess. This keeps the Go
// workflow vendor-agnostic while reusing the proven Python implementation.
// A sidecar service replaces this subprocess wiring in production.
package notebooklm

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
)

// Provider implements genprovider.Provider for OutputAudio. InlineBytes=true;
// async methods return genprovider.ErrNotSupported via the embedded SyncOnly
// mixin.
type Provider struct {
	genprovider.SyncOnly
	PythonBin    string
	ScriptPath   string
	StoragePath  string
	StorageLabel string
	AuthUser     int
	AudioFormat  string
	AudioLength  string
	Language     string
	Timeout      time.Duration
	PollInterval time.Duration
	Now          func() time.Time
}

func New(pythonBin, scriptPath, storagePath string) *Provider {
	return &Provider{
		PythonBin:    pythonBin,
		ScriptPath:   scriptPath,
		StoragePath:  storagePath,
		AuthUser:     1,
		AudioFormat:  "brief",
		AudioLength:  "short",
		Language:     "en",
		Timeout:      8 * time.Minute,
		PollInterval: 5 * time.Second,
		Now:          func() time.Time { return time.Now().UTC() },
	}
}

// Probe returns nil if the bridge can authenticate against NotebookLM.
func (p *Provider) Probe(ctx context.Context) error {
	if p.StoragePath == "" {
		return errors.New("notebooklm: storage_state path required")
	}
	if _, err := os.Stat(p.StoragePath); err != nil {
		return fmt.Errorf("notebooklm: storage_state missing: %w", err)
	}
	args := []string{p.ScriptPath, "--probe", "--storage-state", p.StoragePath}
	if p.StorageLabel != "" {
		args = append(args, "--storage-state-display", p.StorageLabel)
	}
	cmd := exec.CommandContext(ctx, p.PythonBin, args...)
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// InlineBytes is true: notebooklm bridge materializes audio bytes on local
// disk; recovery requires writing S3 atomically with the inference claim.
func (p *Provider) InlineBytes() bool { return true }

// Name satisfies genprovider.Named — telemetry tags provider.* metrics with
// this value. Matches MetaProviderKey on returned artifacts.
func (p *Provider) Name() string { return "notebooklm" }

func (p *Provider) GenerateSync(ctx context.Context, spec generation.JobSpec) (generation.Artifact, error) {
	if spec.OutputType != generation.OutputAudio {
		return generation.Artifact{}, generation.Terminal("UNSUPPORTED_OUTPUT", "notebooklm: only audio overviews supported")
	}
	if p.StoragePath == "" || p.PythonBin == "" || p.ScriptPath == "" {
		return generation.Artifact{}, generation.Terminal("NOTEBOOKLM_CONFIG", "notebooklm provider missing config")
	}
	if _, err := os.Stat(p.StoragePath); err != nil {
		return generation.Artifact{}, generation.Terminal("NOTEBOOKLM_AUTH_MISSING",
			"storage_state.json missing; run `make notebooklm-import` on host")
	}

	tmp, err := os.MkdirTemp("", "msg-nblm-*")
	if err != nil {
		return generation.Artifact{}, err
	}
	defer os.RemoveAll(tmp)
	outPath := filepath.Join(tmp, "out.wav")

	cctx := ctx
	cancel := func() {}
	if p.Timeout > 0 {
		cctx, cancel = context.WithTimeout(ctx, p.Timeout)
	}
	defer cancel()

	args := []string{
		p.ScriptPath,
		"--prompt", spec.Prompt,
		"--out", outPath,
		"--storage-state", p.StoragePath,
		"--audio-format", p.AudioFormat,
		"--audio-length", p.AudioLength,
		"--language", p.Language,
		"--authuser", fmt.Sprintf("%d", p.AuthUser),
		"--timeout", fmt.Sprintf("%d", int(p.Timeout.Seconds())),
		"--poll-interval", fmt.Sprintf("%d", int(p.PollInterval.Seconds())),
		"--cleanup-notebook",
	}
	if p.StorageLabel != "" {
		args = append(args, "--storage-state-display", p.StorageLabel)
	}
	cmd := exec.CommandContext(cctx, p.PythonBin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	stderrSnippet := tailString(stderr.String(), 4*1024)
	stdoutSnippet := strings.TrimSpace(stdout.String())

	if runErr != nil {
		exitCode := -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		}
		if err, ok := structuredScriptError(stderr.String()); ok {
			return generation.Artifact{}, err
		}
		code := "NOTEBOOKLM_RUN_ERROR"
		terminal := false
		switch exitCode {
		case 1:
			code = "NOTEBOOKLM_CONFIG"
			terminal = true
		case 2:
			code = "NOTEBOOKLM_RPC_FAILURE"
			terminal = false
		case 3:
			code = "NOTEBOOKLM_UNEXPECTED"
			terminal = false
		}
		err := &generation.Error{
			Code:     code,
			Message:  fmt.Sprintf("exit=%d stderr=%s stdout=%s", exitCode, stderrSnippet, stdoutSnippet),
			Terminal: terminal,
		}
		return generation.Artifact{}, err
	}

	bytesOut, err := os.ReadFile(outPath)
	if err != nil {
		return generation.Artifact{}, fmt.Errorf("notebooklm: read out: %w", err)
	}
	if len(bytesOut) == 0 {
		return generation.Artifact{}, errors.New("notebooklm: empty audio output")
	}

	contentType, extension := detectAudio(bytesOut)
	sum := sha256.Sum256(bytesOut)
	meta := map[string]string{
		genprovider.MetaProviderKey:         "notebooklm",
		"audio_format":                      p.AudioFormat,
		"audio_length":                      p.AudioLength,
		"language":                          p.Language,
		genprovider.MetaIsAIGeneratedKey:    "true",
		genprovider.MetaDisclosureKey:       genprovider.DisclosureAIGenerated,
		genprovider.MetaVisibleWatermarkKey: "n/a-audio",
		genprovider.MetaContentSafetyKey:    "notebooklm_default",
	}
	if line := lastJSONLine(stdoutSnippet); line != "" {
		var summary map[string]any
		if err := json.Unmarshal([]byte(line), &summary); err == nil {
			if v, ok := summary["notebook_id"].(string); ok {
				meta["notebook_id"] = v
			}
			if v, ok := summary["audio_overview_id"].(string); ok {
				meta["audio_overview_id"] = v
			}
		}
	}

	return generation.Artifact{
		Bytes:       bytesOut,
		ContentType: contentType,
		Extension:   extension,
		SHA256:      hex.EncodeToString(sum[:]),
		Metadata:    meta,
	}, nil
}

type scriptErrorLine struct {
	Code     string `json:"code"`
	Terminal *bool  `json:"terminal"`
	Message  string `json:"message"`
}

func structuredScriptError(stderr string) (*generation.Error, bool) {
	line := lastJSONLine(stderr)
	if line == "" {
		return nil, false
	}
	var scriptErr scriptErrorLine
	if err := json.Unmarshal([]byte(line), &scriptErr); err != nil {
		return nil, false
	}
	if scriptErr.Code == "" || scriptErr.Terminal == nil {
		return nil, false
	}
	return &generation.Error{
		Code:     scriptErr.Code,
		Message:  scriptErr.Message,
		Terminal: *scriptErr.Terminal,
	}, true
}

func detectAudio(b []byte) (string, string) {
	switch {
	case len(b) >= 4 && string(b[:4]) == "RIFF":
		return "audio/wav", "wav"
	case len(b) >= 3 && b[0] == 0xFF && (b[1]&0xE0) == 0xE0:
		return "audio/mpeg", "mp3"
	case len(b) >= 4 && string(b[:4]) == "OggS":
		return "audio/ogg", "ogg"
	case len(b) >= 8 && string(b[4:8]) == "ftyp":
		// ISO BMFF (MP4 family). NotebookLM emits audio-only mp4 with
		// major brands like "dash" / "iso6" / "mp41". Browsers play
		// audio/mp4 natively; .m4a is the conventional extension.
		return "audio/mp4", "m4a"
	}
	return "application/octet-stream", "bin"
}

func tailString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}

func lastJSONLine(s string) string {
	last := ""
	for _, ln := range strings.Split(strings.TrimSpace(s), "\n") {
		ln = strings.TrimSpace(ln)
		if strings.HasPrefix(ln, "{") && strings.HasSuffix(ln, "}") {
			last = ln
		}
	}
	return last
}
