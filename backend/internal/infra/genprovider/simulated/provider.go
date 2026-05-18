// Package simulated provides a deterministic generation provider used in P3
// integration tests and as the default GENERATION_PROVIDER for local mode.
package simulated

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
)

// Provider returns a 64x64 PNG seeded from the prompt. It also offers
// failure-injection hooks for tests covering retry/terminal paths.
// InlineBytes=true; async methods return genprovider.ErrNotSupported via the
// embedded SyncOnly mixin.
type Provider struct {
	genprovider.SyncOnly
	mu       sync.Mutex
	failures map[string]*FailurePlan
}

type FailurePlan struct {
	// TransientFailures decremented on each call; while > 0, GenerateSync
	// returns a transient error.
	TransientFailures int
	// TerminalCode, if set, returns a terminal error every call.
	TerminalCode string
}

func New() *Provider {
	return &Provider{failures: map[string]*FailurePlan{}}
}

// InjectFailures configures the provider to fail for the given clientRequestID
// according to plan. Useful only in tests.
func (p *Provider) InjectFailures(clientRequestID string, plan FailurePlan) {
	p.mu.Lock()
	defer p.mu.Unlock()
	cp := plan
	p.failures[clientRequestID] = &cp
}

// InlineBytes is true: the simulated provider materializes bytes locally;
// crash recovery must persist S3 atomically with the inference claim.
func (p *Provider) InlineBytes() bool { return true }

// Name satisfies genprovider.Named — telemetry tags provider.* metrics with
// this value. Matches MetaProviderKey on returned artifacts.
func (p *Provider) Name() string { return "simulated" }

func (p *Provider) VendorIdempotency() genprovider.VendorIdempotencyMode {
	return genprovider.VendorIdempotencySupported
}

func (p *Provider) GenerateSync(_ context.Context, spec generation.JobSpec) (generation.Artifact, error) {
	if strings.TrimSpace(spec.Prompt) == "" {
		return generation.Artifact{}, generation.Terminal("EMPTY_PROMPT", "prompt is empty")
	}
	p.mu.Lock()
	plan := p.failures[spec.ClientRequestID]
	if plan == nil {
		plan = p.failures[spec.JobID]
	}
	if plan != nil {
		if plan.TerminalCode != "" {
			p.mu.Unlock()
			return generation.Artifact{}, generation.Terminal(plan.TerminalCode, "injected terminal failure")
		}
		if plan.TransientFailures > 0 {
			plan.TransientFailures--
			p.mu.Unlock()
			return generation.Artifact{}, generation.Transient("INJECTED_TRANSIENT", "injected transient failure")
		}
	}
	p.mu.Unlock()

	switch spec.OutputType {
	case generation.OutputAudio:
		return audioArtifact(spec.Prompt)
	default:
		return imageArtifact(spec.Prompt)
	}
}

// imageArtifact renders a 64x64 PNG seeded from the prompt. The watermark
// fingerprint comes from the postprocess stamper in production; here it's a
// placeholder the stamper overrides.
func imageArtifact(prompt string) (generation.Artifact, error) {
	img := renderImage(prompt)
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return generation.Artifact{}, errors.New("simulated: encode png: " + err.Error())
	}
	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return generation.Artifact{
		Bytes:       raw,
		ContentType: "image/png",
		Extension:   "png",
		SHA256:      hex.EncodeToString(sum[:]),
		Metadata: map[string]string{
			genprovider.MetaProviderKey:         "simulated",
			genprovider.MetaVisibleWatermarkKey: "simulated",
			genprovider.MetaContentSafetyKey:    "safe",
			genprovider.MetaIsAIGeneratedKey:    "true",
			genprovider.MetaDisclosureKey:       genprovider.DisclosureAIGenerated,
		},
	}, nil
}

// audioArtifact synthesizes a 1-second silent mono 8 kHz 8-bit PCM WAV.
// 8-bit PCM silence is 0x80, not 0x00.
func audioArtifact(_ string) (generation.Artifact, error) {
	const (
		sampleRate     = 8000
		bytesPerSample = 1
		channels       = 1
		bitsPerSample  = 8
		samples        = sampleRate // 1 second
		dataSize       = samples * bytesPerSample
	)

	buf := bytes.Buffer{}
	buf.Grow(44 + dataSize)
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(36+dataSize))
	buf.WriteString("WAVE")
	buf.WriteString("fmt ")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(16))             // fmt chunk size
	_ = binary.Write(&buf, binary.LittleEndian, uint16(1))              // PCM
	_ = binary.Write(&buf, binary.LittleEndian, uint16(channels))       //
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))     //
	_ = binary.Write(&buf, binary.LittleEndian, uint32(sampleRate))     // byte rate
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bytesPerSample)) // block align
	_ = binary.Write(&buf, binary.LittleEndian, uint16(bitsPerSample))  //
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(dataSize))
	buf.Write(bytes.Repeat([]byte{0x80}, dataSize))

	raw := buf.Bytes()
	sum := sha256.Sum256(raw)
	return generation.Artifact{
		Bytes:       raw,
		ContentType: "audio/wav",
		Extension:   "wav",
		SHA256:      hex.EncodeToString(sum[:]),
		Metadata: map[string]string{
			genprovider.MetaProviderKey:         "simulated",
			genprovider.MetaVisibleWatermarkKey: "n/a-audio",
			genprovider.MetaContentSafetyKey:    "safe",
			genprovider.MetaIsAIGeneratedKey:    "true",
			genprovider.MetaDisclosureKey:       genprovider.DisclosureAIGenerated,
		},
	}, nil
}

func renderImage(prompt string) image.Image {
	const size = 64
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	seed := sha256.Sum256([]byte(prompt))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			r := seed[(y*size+x)%len(seed)]
			g := seed[((y+1)*size+x)%len(seed)]
			b := seed[((y+2)*size+x)%len(seed)]
			img.Set(x, y, color.RGBA{r, g, b, 255})
		}
	}
	return img
}
