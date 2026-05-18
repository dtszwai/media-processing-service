package safety_test

import (
	"context"
	"testing"
	"time"

	safetyapp "github.com/dtszwai/media-processing-service/backend/internal/app/safety"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
)

// fixedClock returns a deterministic clock so CreatedAt assertions don't
// drift across machines. The exact timestamp is irrelevant — only that the
// moderator records the clock it was handed.
func fixedClock(t time.Time) func() time.Time { return func() time.Time { return t } }

func newModerator(t *testing.T) *safetyapp.SimulatedModerator {
	t.Helper()
	m := safetyapp.NewSimulatedModerator()
	m.Now = fixedClock(time.Date(2026, 5, 16, 9, 0, 0, 0, time.UTC))
	m.NewID = func() string { return "vd_fixed" }
	return m
}

func TestSimulatedModerator_Pass_OnCleanPrompt(t *testing.T) {
	m := newModerator(t)
	v, err := m.Moderate(context.Background(), safetyapp.ModerateInput{
		Layer:      safety.LayerInputModeration,
		TenantID:   "tenant-1",
		JobID:      "gen_clean",
		OutputType: generation.OutputImage,
		Prompt:     "a friendly cat in a meadow",
	})
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if v.Decision != safety.DecisionPass {
		t.Fatalf("Decision = %s, want PASS", v.Decision)
	}
	if v.Layer != safety.LayerInputModeration {
		t.Fatalf("Layer = %s, want INPUT_MODERATION", v.Layer)
	}
	if v.SubjectType != "PROMPT" || v.SubjectID != "gen_clean" {
		t.Fatalf("subject = %s/%s, want PROMPT/gen_clean", v.SubjectType, v.SubjectID)
	}
	if v.PolicyVersion == "" || v.Provider != "simulated" {
		t.Fatalf("verdict envelope missing fields: %+v", v)
	}
}

func TestSimulatedModerator_Fail_OnSentinelPrompt(t *testing.T) {
	m := newModerator(t)
	v, err := m.Moderate(context.Background(), safetyapp.ModerateInput{
		Layer:    safety.LayerInputModeration,
		TenantID: "tenant-1",
		JobID:    "gen_bad",
		Prompt:   "harmful query " + safetyapp.SimulatedSentinel + " padding",
	})
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if v.Decision != safety.DecisionFail {
		t.Fatalf("Decision = %s, want FAIL", v.Decision)
	}
	if v.ReasonCode != "MODERATION_SENTINEL_BLOCK" {
		t.Fatalf("ReasonCode = %q", v.ReasonCode)
	}
}

func TestSimulatedModerator_Review_OnSentinelPrompt(t *testing.T) {
	m := newModerator(t)
	v, err := m.Moderate(context.Background(), safetyapp.ModerateInput{
		Layer:    safety.LayerInputModeration,
		TenantID: "tenant-1",
		JobID:    "gen_review",
		Prompt:   "borderline query " + safetyapp.SimulatedReviewSentinel,
	})
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if v.Decision != safety.DecisionReview {
		t.Fatalf("Decision = %s, want REVIEW", v.Decision)
	}
}

func TestSimulatedModerator_Fail_OnArtifactMetadata(t *testing.T) {
	m := newModerator(t)
	art := generation.Artifact{
		SHA256: "abc123",
		Metadata: map[string]string{
			"content_safety": "flag:" + safetyapp.SimulatedSentinel,
		},
	}
	v, err := m.Moderate(context.Background(), safetyapp.ModerateInput{
		Layer:      safety.LayerOutputModeration,
		TenantID:   "tenant-1",
		JobID:      "gen_art_fail",
		OutputType: generation.OutputImage,
		Artifact:   &art,
	})
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if v.Decision != safety.DecisionFail {
		t.Fatalf("Decision = %s, want FAIL", v.Decision)
	}
	if v.SubjectType != "ARTIFACT" || v.SubjectID != "abc123" {
		t.Fatalf("subject = %s/%s, want ARTIFACT/abc123", v.SubjectType, v.SubjectID)
	}
}

func TestSimulatedModerator_Pass_OnCleanArtifact(t *testing.T) {
	m := newModerator(t)
	art := generation.Artifact{
		SHA256: "sha-clean",
		Metadata: map[string]string{
			"content_safety": "safe",
			"disclosure":     "AI_GENERATED_DISCLOSURE",
		},
	}
	v, err := m.Moderate(context.Background(), safetyapp.ModerateInput{
		Layer:      safety.LayerOutputModeration,
		TenantID:   "tenant-1",
		JobID:      "gen_art_pass",
		OutputType: generation.OutputImage,
		Artifact:   &art,
	})
	if err != nil {
		t.Fatalf("Moderate: %v", err)
	}
	if v.Decision != safety.DecisionPass {
		t.Fatalf("Decision = %s, want PASS", v.Decision)
	}
}
