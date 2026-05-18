package idempotency

import "testing"

func TestHashInputsFramesPartBoundaries(t *testing.T) {
	left := HashInputs("a|b", "c")
	right := HashInputs("a", "b|c")
	if left == right {
		t.Fatalf("hash collision across framed inputs: %s", left)
	}
}

func TestHashInputsFramesPartCount(t *testing.T) {
	left := HashInputs("a", "")
	right := HashInputs("a")
	if left == right {
		t.Fatalf("hash collision across different part counts: %s", left)
	}
}
