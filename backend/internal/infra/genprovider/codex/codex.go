// Package codex drives the OpenAI Codex CLI's `codex app-server` JSON-RPC
// subprocess to generate images. Authentication is delegated entirely to the
// CLI (~/.codex/auth.json, refreshed via `codex auth login`); no API key flows
// through this process.
//
// Recovery contract: InlineBytes=true. The provider returns the artifact bytes
// inline so the workflow can write S3 atomically with the inference claim.
package codex

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
)

const (
	defaultModel = "gpt-5.5"
	// defaultTimeout bounds one sync turn. Image generation through
	// `codex app-server` regularly takes 2–5 min end-to-end (thread start,
	// model generation, image saving), and the SQS visibility timeout
	// (1800s) is the outer recovery path.
	defaultTimeout   = 5 * time.Minute
	imageInstruction = "Generate exactly one still image from the user's prompt. " +
		"No text, captions, watermarks, signatures, UI chrome, or follow-up questions. " +
		"No markdown. Return only the image."
)

// Provider implements genprovider.Provider by spawning `codex app-server`.
// Embeds SyncOnly so the async methods short-circuit to ErrNotSupported.
type Provider struct {
	genprovider.SyncOnly
	// Binary is the executable name or full path. Default "codex".
	Binary string
	// Model is the Codex model name used when JobSpec.Model is empty.
	Model string
	// Timeout bounds one GenerateSync call. SQS visibility timeout is the
	// outer recovery path for genuinely stuck calls.
	Timeout time.Duration
}

func New() *Provider {
	return &Provider{Binary: "codex", Model: defaultModel, Timeout: defaultTimeout}
}

func (p *Provider) InlineBytes() bool { return true }

// Name satisfies genprovider.Named — telemetry tags provider.* metrics with
// this value. Matches MetaProviderKey on returned artifacts.
func (p *Provider) Name() string { return "codex" }

// GenerateSync starts a Codex thread, issues one turn instructed to produce
// an image from spec.Prompt, and returns the resulting PNG bytes. The cached
// PNG under ~/.codex/generated_images/{threadID}/ is used as a fallback when
// the Codex turn saves the image to disk instead of returning it inline.
func (p *Provider) GenerateSync(ctx context.Context, spec generation.JobSpec) (generation.Artifact, error) {
	if _, err := exec.LookPath(p.Binary); err != nil {
		return generation.Artifact{}, generation.Terminal("CODEX_BINARY_NOT_FOUND",
			fmt.Sprintf("%q not in PATH: %v — run `codex auth login` on this host", p.Binary, err))
	}

	timeout := p.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	turnCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	model := p.Model
	if spec.Model != "" {
		model = spec.Model
	}

	result, err := p.runTurn(turnCtx, model, spec.Prompt)
	if err != nil {
		return generation.Artifact{}, err
	}

	bytes, err := materializeImage(result)
	if err != nil {
		return generation.Artifact{}, err
	}
	sum := sha256.Sum256(bytes)
	// Watermark + visible disclosure live in postprocess; we only declare
	// the artifact's AI origin so the gate can verify it before publish.
	return generation.Artifact{
		Bytes:       bytes,
		ContentType: "image/png",
		Extension:   "png",
		SHA256:      hex.EncodeToString(sum[:]),
		Metadata: map[string]string{
			genprovider.MetaProviderKey:      "codex",
			genprovider.MetaModelKey:         model,
			genprovider.MetaIsAIGeneratedKey: "true",
			genprovider.MetaDisclosureKey:    genprovider.DisclosureAIGenerated,
			genprovider.MetaContentSafetyKey: "codex_default",
		},
	}, nil
}

// turnResult is the subset of codex app-server output we consume.
type turnResult struct {
	ThreadID       string
	ImageBase64    string
	ImageSavedPath string
}

func (p *Provider) runTurn(ctx context.Context, model, prompt string) (turnResult, error) {
	if strings.TrimSpace(prompt) == "" {
		return turnResult{}, generation.Terminal("CODEX_EMPTY_PROMPT", "prompt is empty")
	}

	cmd := exec.CommandContext(ctx, p.Binary, "app-server", "-c", "mcp_servers={}", "--disable", "plugins")
	cmd.Dir = os.TempDir()
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return turnResult{}, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return turnResult{}, err
	}
	if err := cmd.Start(); err != nil {
		return turnResult{}, generation.Transient("CODEX_START_FAILED", err.Error())
	}
	// Kill the whole process group on context cancel so a hung `codex
	// app-server` doesn't outlive the parent context.
	defer killGroup(cmd)

	rpc := &rpcSession{
		enc:    json.NewEncoder(stdin),
		reader: bufio.NewReaderSize(stdout, 256*1024),
	}

	if _, err := rpc.request(1, "initialize", initParams()); err != nil {
		return turnResult{}, generation.Transient("CODEX_INIT_FAILED", err.Error())
	}
	if err := rpc.notify("initialized", nil); err != nil {
		return turnResult{}, generation.Transient("CODEX_INITIALIZED_FAILED", err.Error())
	}

	threadRaw, err := rpc.request(2, "thread/start", map[string]any{
		"model":                 model,
		"cwd":                   cmd.Dir,
		"approvalPolicy":        "never",
		"approvalsReviewer":     "user",
		"sandbox":               "read-only",
		"developerInstructions": imageInstruction,
		"ephemeral":             true,
		"serviceName":           "media-processing-service",
	})
	if err != nil {
		return turnResult{}, generation.Transient("CODEX_THREAD_START_FAILED", err.Error())
	}
	var threadResp struct {
		Thread struct {
			ID string `json:"id"`
		} `json:"thread"`
	}
	if err := json.Unmarshal(threadRaw, &threadResp); err != nil || threadResp.Thread.ID == "" {
		return turnResult{}, generation.Transient("CODEX_THREAD_START_DECODE",
			fmt.Sprintf("decode thread/start: %v", err))
	}

	if _, err := rpc.request(3, "turn/start", map[string]any{
		"threadId":       threadResp.Thread.ID,
		"input":          []map[string]any{{"type": "text", "text": prompt}},
		"model":          model,
		"approvalPolicy": "never",
		"sandboxPolicy":  map[string]any{"type": "readOnly"},
	}); err != nil {
		return turnResult{}, generation.Transient("CODEX_TURN_START_FAILED", err.Error())
	}

	return rpc.waitForTurn(ctx, threadResp.Thread.ID)
}

// materializeImage prefers the inline base64 result and falls back to the
// on-disk PNG the Codex CLI caches under ~/.codex/generated_images/{threadID}.
func materializeImage(result turnResult) ([]byte, error) {
	if result.ImageBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(result.ImageBase64)
		if err == nil && len(decoded) > 0 {
			return decoded, nil
		}
	}
	if result.ImageSavedPath != "" {
		if b, err := os.ReadFile(result.ImageSavedPath); err == nil && len(b) > 0 {
			return b, nil
		}
	}
	if result.ThreadID != "" {
		if path := latestCachedImage(result.ThreadID); path != "" {
			if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
				return b, nil
			}
		}
	}
	return nil, generation.Transient("CODEX_NO_IMAGE", "codex turn completed with no image output")
}

func latestCachedImage(threadID string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	matches, err := filepath.Glob(filepath.Join(home, ".codex", "generated_images", threadID, "*.png"))
	if err != nil || len(matches) == 0 {
		return ""
	}
	newest, newestAt := matches[0], time.Time{}
	for _, m := range matches {
		info, err := os.Stat(m)
		if err != nil {
			continue
		}
		if newestAt.IsZero() || info.ModTime().After(newestAt) {
			newest, newestAt = m, info.ModTime()
		}
	}
	return newest
}

func initParams() map[string]any {
	return map[string]any{
		"clientInfo": map[string]any{
			"name":    "media-processing-service",
			"title":   "media-processing-service",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"experimentalApi": false,
			"optOutNotificationMethods": []string{
				"mcpServer/startupStatus/updated",
				"skills/changed",
				"thread/status/changed",
			},
		},
	}
}

func killGroup(cmd *exec.Cmd) {
	if cmd.Process == nil {
		return
	}
	_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}
