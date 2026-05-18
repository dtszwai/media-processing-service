package generation_test

import (
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func TestStatusCancelledConstant(t *testing.T) {
	if generation.StatusCancelled != "CANCELLED" {
		t.Fatalf("StatusCancelled drifted: got %q", generation.StatusCancelled)
	}
	// Sanity-check the pre-existing Blocked sentinel the new aggregates rely on.
	if generation.StatusBlocked != "BLOCKED" {
		t.Fatalf("StatusBlocked drifted: got %q", generation.StatusBlocked)
	}
}

func TestGenerationModeConstants(t *testing.T) {
	cases := map[generation.GenerationMode]string{
		generation.GenerationModeCreate: "CREATE",
		generation.GenerationModeEdit:   "EDIT",
		generation.GenerationModeRerun:  "RERUN",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Fatalf("GenerationMode drift: got %q want %q", got, want)
		}
	}
}

func TestEditEnumConstants(t *testing.T) {
	if generation.EditTypePromptDelta != "PROMPT_DELTA" {
		t.Fatalf("EditTypePromptDelta drift")
	}
	if generation.EditBillingDiscount != "EDIT_DISCOUNT" {
		t.Fatalf("EditBillingDiscount drift")
	}
}

func TestAggregateZeroValuesAreUsable(t *testing.T) {
	// Compile-time check: the new aggregates must be addressable struct types
	// with the fields downstream slices will project onto. A zero value is the
	// least-surprising default; if anyone makes one of these an interface or
	// adds an init-required field this test will stop compiling.
	var (
		g generation.Generation
		o generation.Output
		v generation.Variant
		e generation.Edit
	)
	g.OutputType = generation.OutputImage
	o.Type = generation.OutputImage
	v.Index = 0
	e.Type = generation.EditTypePromptDelta
	if g.OutputType != o.Type {
		t.Fatalf("aggregate field wiring mismatch")
	}
}
