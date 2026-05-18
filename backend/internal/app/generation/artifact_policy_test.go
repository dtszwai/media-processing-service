package generation_test

import (
	"testing"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func TestValidateProviderArtifact_AcceptsValidImage(t *testing.T) {
	err := gen.ValidateProviderArtifact(gen.ArtifactPolicyInput{
		JobID:      "gen_1",
		Provider:   "p",
		OutputType: generation.OutputImage,
		Artifact:   generation.Artifact{ContentType: "image/png", Extension: "png"},
	})
	if err != nil {
		t.Fatalf("good image rejected: %v", err)
	}
}

func TestValidateProviderArtifact_AcceptsValidAudio(t *testing.T) {
	err := gen.ValidateProviderArtifact(gen.ArtifactPolicyInput{
		JobID:      "gen_1",
		Provider:   "p",
		OutputType: generation.OutputAudio,
		Artifact:   generation.Artifact{ContentType: "audio/mpeg", Extension: "mp3"},
	})
	if err != nil {
		t.Fatalf("good audio rejected: %v", err)
	}
}

func TestValidateProviderArtifact_RejectsBadInputs(t *testing.T) {
	cases := []struct {
		name string
		in   gen.ArtifactPolicyInput
		code string
	}{
		{
			name: "empty content type",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputImage,
				Artifact:   generation.Artifact{ContentType: "", Extension: "png"},
			},
			code: "ARTIFACT_CONTENT_TYPE_MISSING",
		},
		{
			name: "whitespace content type",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputImage,
				Artifact:   generation.Artifact{ContentType: "   ", Extension: "png"},
			},
			code: "ARTIFACT_CONTENT_TYPE_MISSING",
		},
		{
			name: "octet-stream content type",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputAudio,
				Artifact:   generation.Artifact{ContentType: "application/octet-stream", Extension: "mp3"},
			},
			code: "ARTIFACT_CONTENT_TYPE_GENERIC",
		},
		{
			name: "octet-stream mixed case",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputAudio,
				Artifact:   generation.Artifact{ContentType: "Application/Octet-Stream", Extension: "mp3"},
			},
			code: "ARTIFACT_CONTENT_TYPE_GENERIC",
		},
		{
			name: "empty extension",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputImage,
				Artifact:   generation.Artifact{ContentType: "image/png", Extension: ""},
			},
			code: "ARTIFACT_EXTENSION_INVALID",
		},
		{
			name: "bin extension",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputAudio,
				Artifact:   generation.Artifact{ContentType: "audio/mpeg", Extension: "bin"},
			},
			code: "ARTIFACT_EXTENSION_INVALID",
		},
		{
			name: "image output with audio content type",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputImage,
				Artifact:   generation.Artifact{ContentType: "audio/mpeg", Extension: "mp3"},
			},
			code: "ARTIFACT_CONTENT_TYPE_MISMATCH",
		},
		{
			name: "audio output with image content type",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputAudio,
				Artifact:   generation.Artifact{ContentType: "image/png", Extension: "png"},
			},
			code: "ARTIFACT_CONTENT_TYPE_MISMATCH",
		},
		{
			name: "image output with executable-looking extension",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputImage,
				Artifact:   generation.Artifact{ContentType: "image/png", Extension: "exe"},
			},
			code: "ARTIFACT_EXTENSION_MISMATCH",
		},
		{
			name: "audio output with image extension",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputAudio,
				Artifact:   generation.Artifact{ContentType: "audio/mpeg", Extension: "png"},
			},
			code: "ARTIFACT_EXTENSION_MISMATCH",
		},
		{
			name: "unsupported output type",
			in: gen.ArtifactPolicyInput{
				OutputType: generation.OutputType("VIDEO"),
				Artifact:   generation.Artifact{ContentType: "video/mp4", Extension: "mp4"},
			},
			code: "ARTIFACT_OUTPUT_TYPE_UNSUPPORTED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := gen.ValidateProviderArtifact(tc.in)
			if err == nil {
				t.Fatalf("expected reject for %s, got nil", tc.name)
			}
			if !generation.IsTerminal(err) {
				t.Fatalf("expected terminal error for %s, got %v", tc.name, err)
			}
			if got := generation.AsError(err).Code; got != tc.code {
				t.Fatalf("code = %q, want %q (err=%v)", got, tc.code, err)
			}
		})
	}
}
