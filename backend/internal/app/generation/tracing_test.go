package generation

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestTraceparentRoundTrip(t *testing.T) {
	ctx := ContextWithTraceparent(context.Background(), "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")

	got := TraceparentFromContext(ctx)
	if got != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("TraceparentFromContext = %q", got)
	}
}

func TestTraceparentFromActiveSpan(t *testing.T) {
	traceID := trace.TraceID{0x4b, 0xf9, 0x2f, 0x35, 0x77, 0xb3, 0x4d, 0xa6, 0xa3, 0xce, 0x92, 0x9d, 0x0e, 0x0e, 0x47, 0x36}
	spanID := trace.SpanID{0x00, 0xf0, 0x67, 0xaa, 0x0b, 0xa9, 0x02, 0xb7}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: trace.FlagsSampled,
	}))

	got := TraceparentFromContext(ctx)
	if got != "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01" {
		t.Fatalf("TraceparentFromContext = %q", got)
	}
}

func TestTraceIDFromTraceparent(t *testing.T) {
	got := TraceIDFromTraceparent("00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("TraceIDFromTraceparent = %q", got)
	}

	if TraceIDFromTraceparent("00-00000000000000000000000000000000-00f067aa0ba902b7-01") != "" {
		t.Fatalf("invalid trace id should return empty")
	}
}
