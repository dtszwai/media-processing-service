package outbox

import (
	"testing"
	"time"
)

func TestEnqueuedAtFromSKParsesTimestampPrefix(t *testing.T) {
	ts := time.Date(2026, 5, 17, 20, 51, 50, 123456789, time.UTC)
	sk := ts.Format(time.RFC3339Nano) + "#gen_1#PROVIDER_SUBMIT"

	if got := enqueuedAtFromSK(sk); got != ts.Format(time.RFC3339Nano) {
		t.Fatalf("enqueuedAtFromSK = %q, want %q", got, ts.Format(time.RFC3339Nano))
	}
}

func TestEnqueuedAtFromSKRejectsMalformedPrefix(t *testing.T) {
	if got := enqueuedAtFromSK("not-a-time#gen_1#PROVIDER_SUBMIT"); got != "" {
		t.Fatalf("enqueuedAtFromSK = %q, want empty", got)
	}
}
