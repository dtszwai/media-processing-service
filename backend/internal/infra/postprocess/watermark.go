// Package postprocess hosts byte-level mutations the generation workflow
// applies between the staged artifact (raw provider output) and the final
// asset write. The AI-disclosure invariant (AGENTS.md: "Images must carry
// watermark + disclosure metadata before storage upload") is enforced here:
// images flow through Stamper.StampImage which paints a visible watermark
// onto the bytes themselves and records a fingerprint the gate verifies.
package postprocess

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strconv"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// WatermarkAlgo is the public identifier baked into the artifact metadata
// under the `watermark.algo` key. Bumping the version invalidates older
// stamped artifacts at the gate level (callers can rev `WatermarkAlgo` and
// reject any artifact carrying an older value).
const WatermarkAlgo = "visible-text-v1"

// MinTextLength is the lower bound on the visible watermark label. We refuse
// to stamp shorter labels so the gate cannot be tricked by empty/whitespace
// markers like " " or "x" that satisfy "non-empty" but are imperceptible.
const MinTextLength = 4

const (
	MaxPNGInputBytes  = 32 * 1024 * 1024
	MaxPNGInputPixels = 4096 * 4096
)

// MetadataKeys is the set of artifact metadata keys this package writes.
// Centralized so the gate can reject any of them being missing/placeholder
// without duplicating the string literals.
var MetadataKeys = struct {
	Algo        string
	Fingerprint string
	Position    string
	Font        string
	Text        string
}{
	Algo:        "watermark.algo",
	Fingerprint: "watermark.fingerprint",
	Position:    "watermark.position",
	Font:        "watermark.font",
	Text:        "watermark.text",
}

// Position controls where the watermark sits on the image. We expose only
// values that survive arbitrary aspect ratios — corners — so the same
// policy can apply to portrait and landscape without per-output tuning.
type Position string

const (
	PositionBottomRight Position = "bottom-right"
	PositionBottomLeft  Position = "bottom-left"
	PositionTopRight    Position = "top-right"
	PositionTopLeft     Position = "top-left"
)

// Stamper paints a text watermark onto a PNG-encoded image. The result is
// a new PNG with the bytes mutated; the fingerprint returned by StampImage
// is the SHA-256 of the stamped bytes (NOT of the watermark glyphs) and is
// what the gate compares against.
type Stamper struct {
	// Text is the visible label rendered onto the image. Should be a stable
	// short phrase like "AI Generated" — change discipline matters because
	// every existing stamped asset gates against the algo+text pair.
	Text string
	// Position controls glyph placement. PositionBottomRight by default.
	Position Position
	// Opacity is the alpha applied to the text. 0..255; defaults to ~75% so
	// the mark is visible without dominating the image.
	Opacity uint8
}

// NewStamper returns a Stamper with defaults tuned for production: bottom-
// right placement, ~75% opacity, fail-loud on empty text. The text label
// must be at least MinTextLength characters or NewStamper returns an error
// (callers should treat that as a config bug, not a runtime error).
func NewStamper(text string) (*Stamper, error) {
	if len(text) < MinTextLength {
		return nil, fmt.Errorf("postprocess: watermark text %q below MinTextLength=%d", text, MinTextLength)
	}
	return &Stamper{
		Text:     text,
		Position: PositionBottomRight,
		Opacity:  192,
	}, nil
}

// StampImage decodes pngBytes, draws the watermark, and returns the new
// PNG bytes plus the metadata the gate validates. Errors decode/encode
// failures so the caller can route them through the workflow's transient/
// terminal classifier.
//
// Memory: DecodeConfig lets us reject oversized input before allocating the
// decoded pixel buffer. When png.Decode already returns *image.RGBA we mutate
// in place; other colour models cost one extra RGBA buffer for the conversion.
func (s *Stamper) StampImage(pngBytes []byte) ([]byte, map[string]string, error) {
	if len(s.Text) < MinTextLength {
		return nil, nil, fmt.Errorf("postprocess: stamper text %q below MinTextLength=%d", s.Text, MinTextLength)
	}
	if len(pngBytes) > MaxPNGInputBytes {
		return nil, nil, fmt.Errorf("postprocess: png input bytes %d exceeds cap %d", len(pngBytes), MaxPNGInputBytes)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("postprocess: decode png config: %w", err)
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if cfg.Width <= 0 || cfg.Height <= 0 || pixels > MaxPNGInputPixels {
		return nil, nil, fmt.Errorf("postprocess: png dimensions %dx%d exceed pixel cap %d", cfg.Width, cfg.Height, MaxPNGInputPixels)
	}
	src, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, nil, fmt.Errorf("postprocess: decode png: %w", err)
	}
	bounds := src.Bounds()
	dst, ok := src.(*image.RGBA)
	if !ok {
		dst = image.NewRGBA(bounds)
		draw.Draw(dst, bounds, src, bounds.Min, draw.Src)
	}

	face := basicfont.Face7x13
	textW, textH := measureText(face, s.Text)
	const pad = 8
	var x, y int
	switch s.Position {
	case PositionTopLeft:
		x = bounds.Min.X + pad
		y = bounds.Min.Y + pad + textH
	case PositionTopRight:
		x = bounds.Max.X - textW - pad
		y = bounds.Min.Y + pad + textH
	case PositionBottomLeft:
		x = bounds.Min.X + pad
		y = bounds.Max.Y - pad
	case PositionBottomRight:
		x = bounds.Max.X - textW - pad
		y = bounds.Max.Y - pad
	default:
		return nil, nil, fmt.Errorf("postprocess: unknown watermark position %q", s.Position)
	}

	// Draw a translucent backing box so the text remains legible on any
	// background. The box is sized to the text plus a small bleed.
	boxBleed := 3
	boxColor := color.RGBA{0, 0, 0, s.Opacity / 2}
	box := image.Rect(x-boxBleed, y-textH-boxBleed, x+textW+boxBleed, y+boxBleed)
	if box.Min.X < bounds.Min.X {
		box.Min.X = bounds.Min.X
	}
	if box.Min.Y < bounds.Min.Y {
		box.Min.Y = bounds.Min.Y
	}
	if box.Max.X > bounds.Max.X {
		box.Max.X = bounds.Max.X
	}
	if box.Max.Y > bounds.Max.Y {
		box.Max.Y = bounds.Max.Y
	}
	draw.Draw(dst, box, &image.Uniform{C: boxColor}, image.Point{}, draw.Over)

	textColor := color.RGBA{255, 255, 255, s.Opacity}
	drawer := &font.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(textColor),
		Face: face,
		Dot:  fixed.Point26_6{X: fixed.I(x), Y: fixed.I(y)},
	}
	drawer.DrawString(s.Text)

	var buf bytes.Buffer
	if err := png.Encode(&buf, dst); err != nil {
		return nil, nil, fmt.Errorf("postprocess: encode png: %w", err)
	}
	stampedBytes := buf.Bytes()
	sum := sha256.Sum256(stampedBytes)
	fingerprint := hex.EncodeToString(sum[:])

	meta := map[string]string{
		MetadataKeys.Algo:        WatermarkAlgo,
		MetadataKeys.Fingerprint: fingerprint,
		MetadataKeys.Position:    string(s.Position),
		MetadataKeys.Font:        "basicfont.Face7x13",
		MetadataKeys.Text:        s.Text,
		"watermark.opacity":      strconv.Itoa(int(s.Opacity)),
	}
	return stampedBytes, meta, nil
}

// measureText returns text width/height for the given bitmap face.
func measureText(face font.Face, s string) (int, int) {
	advance := font.MeasureString(face, s).Ceil()
	height := face.Metrics().Height.Ceil()
	return advance, height
}
