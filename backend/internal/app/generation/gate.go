package generation

import (
	"slices"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/genprovider"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/postprocess"
)

// VerifyPublishableArtifact enforces the AI-disclosure invariant before the
// workflow promotes a provider artifact to a customer-visible Asset row.
// Returns a Terminal classified error so the workflow does not retry.
//
// Image: visible_watermark must be a real fingerprint (computed by the
// postprocess stamper), watermark.algo must match the current stamper,
// disclosure and content_safety must be non-placeholder.
// Audio: disclosure + content_safety. Watermark is "n/a-audio" by convention.
//
// Placeholders are rejected aggressively: any value that starts with `TODO_`,
// matches a known sentinel ("placeholder", "simulated", "none", "n/a"…), or
// is whitespace fails the check.
func VerifyPublishableArtifact(art generation.Artifact, output generation.OutputType) error {
	m := art.Metadata
	if m == nil {
		m = map[string]string{}
	}
	switch output {
	case generation.OutputImage:
		if !isWatermarkFingerprint(m[postprocess.MetadataKeys.Fingerprint]) {
			return generation.Terminal("WATERMARK_FINGERPRINT_MISSING",
				"image artifact lacks watermark.fingerprint (postprocess stamper did not run)")
		}
		if m[postprocess.MetadataKeys.Algo] != postprocess.WatermarkAlgo {
			return generation.Terminal("WATERMARK_ALGO_MISMATCH",
				"image artifact watermark.algo does not match current stamper")
		}
		if !isPublishableMetadata(m[genprovider.MetaVisibleWatermarkKey]) {
			return generation.Terminal("WATERMARK_MISSING",
				"image artifact visible_watermark is placeholder or empty")
		}
		if !isPublishableMetadata(m[genprovider.MetaDisclosureKey]) {
			return generation.Terminal("AI_DISCLOSURE_MISSING",
				"image artifact lacks disclosure metadata")
		}
		if !isPublishableMetadata(m[genprovider.MetaContentSafetyKey]) {
			return generation.Terminal("OUTPUT_SAFETY_MISSING",
				"image artifact lacks content_safety metadata")
		}
	case generation.OutputAudio:
		if !isPublishableMetadata(m[genprovider.MetaDisclosureKey]) {
			return generation.Terminal("AI_DISCLOSURE_MISSING",
				"audio artifact lacks disclosure metadata")
		}
		if !isPublishableMetadata(m[genprovider.MetaContentSafetyKey]) {
			return generation.Terminal("OUTPUT_SAFETY_MISSING",
				"audio artifact lacks content_safety metadata")
		}
	}
	return nil
}

// placeholderSentinels are values the gate refuses as substitutes for a
// real disclosure decision. Compared case-insensitively after trimming.
var placeholderSentinels = []string{"placeholder", "none", "n/a", "nil", "null", "simulated"}

// isPublishableMetadata returns false for empty, whitespace, or sentinel
// placeholder values.
func isPublishableMetadata(v string) bool {
	s := strings.TrimSpace(v)
	if s == "" {
		return false
	}
	if strings.HasPrefix(s, "TODO_") || strings.HasPrefix(s, "todo_") {
		return false
	}
	return !slices.Contains(placeholderSentinels, strings.ToLower(s))
}

// isWatermarkFingerprint returns true iff v is a 64-char lowercase hex
// SHA-256 — exactly the shape the postprocess stamper emits.
func isWatermarkFingerprint(v string) bool {
	if len(v) != 64 {
		return false
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
