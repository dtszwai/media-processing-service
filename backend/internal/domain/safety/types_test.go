package safety_test

import (
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/safety"
)

func TestDecisionConstants(t *testing.T) {
	cases := map[safety.Decision]string{
		safety.DecisionPass:   "PASS",
		safety.DecisionFail:   "FAIL",
		safety.DecisionReview: "REVIEW",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Fatalf("Decision drift: got %q want %q", got, want)
		}
	}
}

func TestLayerConstants(t *testing.T) {
	if safety.LayerInputModeration != "INPUT_MODERATION" {
		t.Fatalf("LayerInputModeration drift")
	}
	if safety.LayerOutputModeration != "OUTPUT_MODERATION" {
		t.Fatalf("LayerOutputModeration drift")
	}
	if safety.LayerDisclosureGate != "DISCLOSURE_GATE" {
		t.Fatalf("LayerDisclosureGate drift")
	}
}

func TestVerdictAndGateZeroValues(t *testing.T) {
	v := safety.Verdict{Decision: safety.DecisionPass, Layer: safety.LayerInputModeration}
	if v.Decision != safety.DecisionPass {
		t.Fatalf("verdict round-trip")
	}
	g := safety.DisclosureGateDecision{
		Decision:          safety.DecisionPass,
		DisclosurePresent: true,
		WatermarkPresent:  true,
		SafetyPresent:     true,
		ManifestPresent:   true,
	}
	if !(g.DisclosurePresent && g.WatermarkPresent && g.SafetyPresent && g.ManifestPresent) {
		t.Fatalf("gate-pass requires all four signals present")
	}
}
