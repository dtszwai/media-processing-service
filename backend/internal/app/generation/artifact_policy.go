package generation

import (
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// ArtifactPolicyInput is the content-class policy contract the postprocess
// stage stakes before final storage. Distinct from VerifyPublishableArtifact
// (which gates AI-disclosure metadata): this gate enforces that the bytes
// themselves can be served as the declared OutputType.
type ArtifactPolicyInput struct {
	JobID      string
	Provider   string
	OutputType generation.OutputType
	Stage      generation.Stage
	Artifact   generation.Artifact
}

// ValidateProviderArtifact rejects artifacts whose declared content type or
// extension would let a producer smuggle generic binary into the canonical
// storage path. Always Terminal so the workflow does not retry: the producer
// is the only party that can fix a malformed output.
//
// Rules:
//   - ContentType non-empty and not application/octet-stream
//   - Extension non-empty and not the generic "bin" fallback
//   - ContentType class and extension must match OutputType
func ValidateProviderArtifact(in ArtifactPolicyInput) error {
	ct := strings.ToLower(strings.TrimSpace(in.Artifact.ContentType))
	ext := strings.ToLower(strings.TrimSpace(in.Artifact.Extension))
	ext = strings.TrimPrefix(ext, ".")

	if ct == "" {
		return generation.Terminal("ARTIFACT_CONTENT_TYPE_MISSING",
			"provider artifact has no content_type")
	}
	if ct == "application/octet-stream" {
		return generation.Terminal("ARTIFACT_CONTENT_TYPE_GENERIC",
			"provider artifact returned application/octet-stream — output type unknown")
	}
	if ext == "" || ext == "bin" {
		return generation.Terminal("ARTIFACT_EXTENSION_INVALID",
			"provider artifact extension is empty or generic")
	}

	var classOK, extOK bool
	switch in.OutputType {
	case generation.OutputImage:
		classOK = strings.HasPrefix(ct, "image/")
		extOK = ext == "png" || ext == "jpg" || ext == "jpeg" || ext == "webp"
	case generation.OutputAudio:
		classOK = strings.HasPrefix(ct, "audio/")
		extOK = ext == "mp3" || ext == "mpeg" || ext == "wav" || ext == "m4a" || ext == "aac" || ext == "ogg" || ext == "opus"
	default:
		return generation.Terminal("ARTIFACT_OUTPUT_TYPE_UNSUPPORTED",
			"unsupported output_type: "+string(in.OutputType))
	}
	if !classOK {
		return generation.Terminal("ARTIFACT_CONTENT_TYPE_MISMATCH",
			"content_type "+ct+" does not match output_type "+string(in.OutputType))
	}
	if !extOK {
		return generation.Terminal("ARTIFACT_EXTENSION_MISMATCH",
			"extension "+ext+" does not match output_type "+string(in.OutputType))
	}
	return nil
}
