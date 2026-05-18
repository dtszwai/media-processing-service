package generation_test

import (
	"strings"
	"testing"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/postprocess"
)

// realFingerprint is a 64-char lowercase hex string the gate accepts as
// a watermark.fingerprint value. The exact bytes don't matter — only the
// shape does.
const realFingerprint = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"

func goodImageMeta() map[string]string {
	return map[string]string{
		postprocess.MetadataKeys.Algo:        postprocess.WatermarkAlgo,
		postprocess.MetadataKeys.Fingerprint: realFingerprint,
		"visible_watermark":                  realFingerprint,
		"content_safety":                     "safe",
		"disclosure":                         "AI_GENERATED_DISCLOSURE",
	}
}

func TestVerifyPublishableArtifact_Image(t *testing.T) {
	good := generation.Artifact{Metadata: goodImageMeta()}
	if err := gen.VerifyPublishableArtifact(good, generation.OutputImage); err != nil {
		t.Fatalf("good image flagged: %v", err)
	}

	// Missing fingerprint → terminal WATERMARK_FINGERPRINT_MISSING.
	noFP := generation.Artifact{Metadata: goodImageMeta()}
	delete(noFP.Metadata, postprocess.MetadataKeys.Fingerprint)
	if err := gen.VerifyPublishableArtifact(noFP, generation.OutputImage); err == nil || !generation.IsTerminal(err) {
		t.Fatalf("missing fingerprint must terminal-reject: %v", err)
	}

	// Algo mismatch → terminal WATERMARK_ALGO_MISMATCH.
	wrongAlgo := generation.Artifact{Metadata: goodImageMeta()}
	wrongAlgo.Metadata[postprocess.MetadataKeys.Algo] = "different-algo"
	if err := gen.VerifyPublishableArtifact(wrongAlgo, generation.OutputImage); err == nil || !generation.IsTerminal(err) {
		t.Fatalf("algo mismatch must terminal-reject: %v", err)
	}

	// Disclosure missing → terminal AI_DISCLOSURE_MISSING.
	noDisclose := generation.Artifact{Metadata: goodImageMeta()}
	delete(noDisclose.Metadata, "disclosure")
	if err := gen.VerifyPublishableArtifact(noDisclose, generation.OutputImage); err == nil || !generation.IsTerminal(err) {
		t.Fatalf("missing disclosure must terminal-reject: %v", err)
	}

	// Safety missing → terminal OUTPUT_SAFETY_MISSING.
	noSafety := generation.Artifact{Metadata: goodImageMeta()}
	delete(noSafety.Metadata, "content_safety")
	if err := gen.VerifyPublishableArtifact(noSafety, generation.OutputImage); err == nil {
		t.Fatalf("missing safety must reject")
	}
}

// TestVerifyPublishableArtifact_Image_RejectsPlaceholders enforces the
// AI-disclosure invariant:
//   - watermark.fingerprint must be a real 64-char hex (set by stamper).
//   - visible_watermark must not be a placeholder sentinel.
//   - disclosure/content_safety must not be placeholder sentinels.
func TestVerifyPublishableArtifact_Image_RejectsPlaceholders(t *testing.T) {
	cases := []struct {
		name     string
		mutate   func(m map[string]string)
		wantCode string
	}{
		{
			name: "fingerprint=TODO sentinel",
			mutate: func(m map[string]string) {
				m[postprocess.MetadataKeys.Fingerprint] = "TODO_apply_in_postprocess"
			},
			wantCode: "WATERMARK_FINGERPRINT_MISSING",
		},
		{
			name: "fingerprint=non-hex",
			mutate: func(m map[string]string) {
				m[postprocess.MetadataKeys.Fingerprint] = strings.Repeat("z", 64)
			},
			wantCode: "WATERMARK_FINGERPRINT_MISSING",
		},
		{
			name: "fingerprint=short",
			mutate: func(m map[string]string) {
				m[postprocess.MetadataKeys.Fingerprint] = "deadbeef"
			},
			wantCode: "WATERMARK_FINGERPRINT_MISSING",
		},
		{
			name: "fingerprint=empty",
			mutate: func(m map[string]string) {
				m[postprocess.MetadataKeys.Fingerprint] = ""
			},
			wantCode: "WATERMARK_FINGERPRINT_MISSING",
		},
		{
			name: "visible_watermark=TODO sentinel",
			mutate: func(m map[string]string) {
				m["visible_watermark"] = "TODO_apply_in_postprocess"
			},
			wantCode: "WATERMARK_MISSING",
		},
		{
			name: "visible_watermark=placeholder",
			mutate: func(m map[string]string) {
				m["visible_watermark"] = "placeholder"
			},
			wantCode: "WATERMARK_MISSING",
		},
		{
			name: "visible_watermark=simulated",
			mutate: func(m map[string]string) {
				m["visible_watermark"] = "simulated"
			},
			wantCode: "WATERMARK_MISSING",
		},
		{
			name: "visible_watermark=whitespace",
			mutate: func(m map[string]string) {
				m["visible_watermark"] = "   "
			},
			wantCode: "WATERMARK_MISSING",
		},
		{
			name: "disclosure=TODO",
			mutate: func(m map[string]string) {
				m["disclosure"] = "TODO_set_disclosure"
			},
			wantCode: "AI_DISCLOSURE_MISSING",
		},
		{
			name: "content_safety=placeholder",
			mutate: func(m map[string]string) {
				m["content_safety"] = "placeholder"
			},
			wantCode: "OUTPUT_SAFETY_MISSING",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			meta := goodImageMeta()
			tc.mutate(meta)
			err := gen.VerifyPublishableArtifact(generation.Artifact{Metadata: meta}, generation.OutputImage)
			if err == nil {
				t.Fatalf("expected gate to reject %s, got nil", tc.name)
			}
			if !generation.IsTerminal(err) {
				t.Fatalf("expected terminal error for %s, got %v", tc.name, err)
			}
			if got := generation.AsError(err).Code; got != tc.wantCode {
				t.Fatalf("code = %q, want %q (err=%v)", got, tc.wantCode, err)
			}
		})
	}
}

func TestVerifyPublishableArtifact_Audio(t *testing.T) {
	good := generation.Artifact{Metadata: map[string]string{"disclosure": "AI_GENERATED_DISCLOSURE", "content_safety": "safe"}}
	if err := gen.VerifyPublishableArtifact(good, generation.OutputAudio); err != nil {
		t.Fatalf("good audio flagged: %v", err)
	}
	noDisclose := generation.Artifact{Metadata: map[string]string{"content_safety": "safe"}}
	err := gen.VerifyPublishableArtifact(noDisclose, generation.OutputAudio)
	if err == nil || !generation.IsTerminal(err) {
		t.Fatalf("audio without disclosure must terminal-reject: %v", err)
	}
	// Audio with a TODO disclosure sentinel must also reject.
	todoDisclose := generation.Artifact{Metadata: map[string]string{"disclosure": "TODO_set_disclosure", "content_safety": "safe"}}
	if err := gen.VerifyPublishableArtifact(todoDisclose, generation.OutputAudio); err == nil {
		t.Fatalf("audio with TODO_ disclosure must reject")
	}
}
