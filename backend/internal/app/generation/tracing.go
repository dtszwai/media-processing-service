package generation

import (
	"context"
	"strings"

	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func ContextWithTraceparent(ctx context.Context, traceparent string) context.Context {
	traceparent = strings.TrimSpace(traceparent)
	if traceparent == "" {
		return ctx
	}
	return propagation.TraceContext{}.Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
}

func TraceparentFromContext(ctx context.Context) string {
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

func TraceIDFromTraceparent(traceparent string) string {
	parts := strings.Split(strings.TrimSpace(traceparent), "-")
	if len(parts) < 4 {
		return ""
	}
	traceID, err := trace.TraceIDFromHex(parts[1])
	if err != nil || !traceID.IsValid() {
		return ""
	}
	return traceID.String()
}
