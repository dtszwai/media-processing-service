package postprocess_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash/crc32"
	"image"
	"image/png"
	"strings"
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/postprocess"
)

// renderRGBA returns a real PNG-encoded image of the given size. Tests use
// it to feed the stamper a valid byte stream and inspect output pixels.
func renderRGBA(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	// Fill with a single bright colour so we can later assert that the
	// stamper mutated pixels by comparing input and output bytes.
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Pix[(y*w+x)*4+0] = 200
			img.Pix[(y*w+x)*4+1] = 100
			img.Pix[(y*w+x)*4+2] = 50
			img.Pix[(y*w+x)*4+3] = 255
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic("renderRGBA: " + err.Error())
	}
	return buf.Bytes()
}

// TestStampImage_MutatesBytesAndFingerprint enforces the stamper contract:
//  1. output bytes differ from input,
//  2. fingerprint matches the output SHA-256,
//  3. metadata carries algo + position + text fields.
func TestStampImage_MutatesBytesAndFingerprint(t *testing.T) {
	src := renderRGBA(256, 128)
	stamper, err := postprocess.NewStamper("AI Generated")
	if err != nil {
		t.Fatalf("NewStamper: %v", err)
	}
	out, meta, err := stamper.StampImage(src)
	if err != nil {
		t.Fatalf("StampImage: %v", err)
	}

	if bytes.Equal(src, out) {
		t.Fatalf("output bytes identical to input — stamper did not mutate the image")
	}

	// Fingerprint must equal SHA-256(out).
	sum := sha256.Sum256(out)
	want := hex.EncodeToString(sum[:])
	if got := meta[postprocess.MetadataKeys.Fingerprint]; got != want {
		t.Fatalf("fingerprint = %q, want %q", got, want)
	}

	// Metadata invariants.
	if meta[postprocess.MetadataKeys.Algo] != postprocess.WatermarkAlgo {
		t.Fatalf("algo = %q, want %q", meta[postprocess.MetadataKeys.Algo], postprocess.WatermarkAlgo)
	}
	if meta[postprocess.MetadataKeys.Text] != "AI Generated" {
		t.Fatalf("text = %q, want AI Generated", meta[postprocess.MetadataKeys.Text])
	}
	if meta[postprocess.MetadataKeys.Position] != string(postprocess.PositionBottomRight) {
		t.Fatalf("position = %q, want bottom-right", meta[postprocess.MetadataKeys.Position])
	}

	// Output must still decode as a valid PNG.
	if _, err := png.Decode(bytes.NewReader(out)); err != nil {
		t.Fatalf("output does not decode as PNG: %v", err)
	}
}

// TestStampImage_PixelsActuallyChanged decodes the stamped output and asserts
// some pixels in the bottom-right region differ from the uniform input. This
// is the proof-of-stamp the e2e test relies on; the byte-equality check above
// catches re-encoding artifacts but doesn't prove glyphs were drawn.
func TestStampImage_PixelsActuallyChanged(t *testing.T) {
	src := renderRGBA(128, 64)
	stamper, err := postprocess.NewStamper("AI Generated")
	if err != nil {
		t.Fatalf("NewStamper: %v", err)
	}
	out, _, err := stamper.StampImage(src)
	if err != nil {
		t.Fatalf("StampImage: %v", err)
	}
	stamped, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode stamped: %v", err)
	}
	bounds := stamped.Bounds()
	// Scan the bottom-right quadrant — the stamper places text there by
	// default. At least one pixel must differ from the uniform fill colour
	// (200,100,50,255) for the watermark to be visible.
	differs := 0
	for y := bounds.Max.Y - 24; y < bounds.Max.Y; y++ {
		for x := bounds.Max.X - 96; x < bounds.Max.X; x++ {
			r, g, b, _ := stamped.At(x, y).RGBA()
			if r>>8 != 200 || g>>8 != 100 || b>>8 != 50 {
				differs++
			}
		}
	}
	if differs == 0 {
		t.Fatalf("no pixels differed in the watermark zone — stamper did not draw glyphs")
	}
}

// TestNewStamper_RejectsShortText asserts NewStamper refuses 1-char text so a
// misconfigured stamper cannot ship past the gate.
func TestNewStamper_RejectsShortText(t *testing.T) {
	if _, err := postprocess.NewStamper("x"); err == nil {
		t.Fatalf("expected NewStamper to reject 1-char text")
	}
	if _, err := postprocess.NewStamper(""); err == nil {
		t.Fatalf("expected NewStamper to reject empty text")
	}
	if _, err := postprocess.NewStamper("AI Generated"); err != nil {
		t.Fatalf("NewStamper rejected a valid label: %v", err)
	}
}

// TestStampImage_NonPNGInput surfaces the decode error as a regular Go error
// so the workflow's terminal/transient classifier can route it. The string
// match keeps this test resilient to wrapper changes.
func TestStampImage_NonPNGInput(t *testing.T) {
	stamper, _ := postprocess.NewStamper("AI Generated")
	_, _, err := stamper.StampImage([]byte("not a png"))
	if err == nil {
		t.Fatalf("expected decode error on non-PNG input")
	}
	if !strings.Contains(err.Error(), "decode png config") {
		t.Fatalf("error message = %q, want it to mention decode png config", err.Error())
	}
}

func TestStampImage_RejectsHugeCompressedPNGBeforeDecode(t *testing.T) {
	stamper, _ := postprocess.NewStamper("AI Generated")
	huge := pngHeaderOnly(5000, 5000)

	_, _, err := stamper.StampImage(huge)
	if err == nil {
		t.Fatalf("expected pixel-cap error")
	}
	if !strings.Contains(err.Error(), "exceed pixel cap") {
		t.Fatalf("error message = %q, want pixel cap", err.Error())
	}
}

func TestStampImage_RejectsInputByteCapBeforeDecodeConfig(t *testing.T) {
	stamper, _ := postprocess.NewStamper("AI Generated")
	oversized := make([]byte, postprocess.MaxPNGInputBytes+1)

	_, _, err := stamper.StampImage(oversized)
	if err == nil {
		t.Fatalf("expected byte-cap error")
	}
	if !strings.Contains(err.Error(), "input bytes") {
		t.Fatalf("error message = %q, want input bytes cap", err.Error())
	}
}

func pngHeaderOnly(width, height uint32) []byte {
	var out bytes.Buffer
	out.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:4], width)
	binary.BigEndian.PutUint32(ihdr[4:8], height)
	ihdr[8] = 8
	ihdr[9] = 2
	writePNGChunk(&out, "IHDR", ihdr)
	writePNGChunk(&out, "IEND", nil)
	return out.Bytes()
}

func writePNGChunk(out *bytes.Buffer, typ string, data []byte) {
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	out.Write(lenBuf[:])
	out.WriteString(typ)
	out.Write(data)
	crc := crc32.NewIEEE()
	_, _ = crc.Write([]byte(typ))
	_, _ = crc.Write(data)
	binary.BigEndian.PutUint32(lenBuf[:], crc.Sum32())
	out.Write(lenBuf[:])
}
