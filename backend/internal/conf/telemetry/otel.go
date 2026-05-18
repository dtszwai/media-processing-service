// Package telemetry wires the OpenTelemetry SDK with OTLP exporters for
// traces, metrics, and logs when cfg.Telemetry.OTLPEndpoint is set, plus
// W3C propagators so traceparent headers round-trip end-to-end.
//
// Init also returns a *slog.Logger whose handler fans out to stdout AND
// (when OTLP is wired) the OTel logs bridge — so `docker logs` / CloudWatch
// keep working while Loki gets the same lines with TraceID correlation when
// the call site uses *Context variants (e.g. slog.InfoContext).
package telemetry

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otellog "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"

	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
)

// Init installs the global tracer + meter + logger providers for serviceName
// and returns a *slog.Logger ready to be installed via slog.SetDefault.
//
// The returned logger is always non-nil. The returned shutdown is always
// safe to call. When cfg.Telemetry.OTLPEndpoint is empty the OTLP side is
// skipped and the logger is a plain JSON stdout handler. Set
// cfg.Telemetry.LogsDisabled=true to opt out of OTLP log export only — keeps
// traces+metrics — useful for prod Lambdas that prefer CloudWatch as the
// sole log sink to avoid Loki ingest cost.
func Init(ctx context.Context, serviceName string, cfg app.Config) (*slog.Logger, func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))

	level := parseLevel(cfg.Telemetry.LogLevel)
	stdoutHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})
	stdoutLogger := slog.New(stdoutHandler)

	// Route OTel SDK errors and internal logs through slog at their original
	// severity. Without this, otel.Handle(err) and global.{Error,Warn,Info,Debug}
	// fall back to log.Print, which slog.SetDefault (called by runtime.Init)
	// re-routes into slog at LevelInfo via the stdlib-log → slog bridge —
	// collapsing every SDK error/warning to INFO. Bind to a stdout-only logger:
	// if we used the multiHandler below, an OTLP export failure would re-emit
	// the error through the OTel log exporter and loop.
	otel.SetErrorHandler(otelErrorHandler{logger: stdoutLogger})
	otel.SetLogger(logr.FromSlogHandler(otelSlogHandler{next: stdoutHandler}))

	endpoint := cfg.Telemetry.OTLPEndpoint
	if endpoint == "" {
		return stdoutLogger, func(context.Context) error { return nil }, nil
	}
	target := stripScheme(endpoint)

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.DeploymentEnvironmentName(cfg.Env),
		),
	)
	if err != nil {
		return stdoutLogger, func(context.Context) error { return nil }, err
	}

	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(target),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return stdoutLogger, func(context.Context) error { return nil }, err
	}
	sampler := cfg.Telemetry.TracesSampler
	if sampler <= 0 {
		sampler = 1.0
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(sampler))),
	)
	otel.SetTracerProvider(tp)

	shutdownTrace := func(c context.Context) error { return tp.Shutdown(c) }

	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(target),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return stdoutLogger, shutdownTrace, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(15*time.Second))),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	shutdownTraceMetric := func(c context.Context) error {
		_ = mp.Shutdown(c)
		return tp.Shutdown(c)
	}

	if cfg.Telemetry.LogsDisabled {
		return stdoutLogger, shutdownTraceMetric, nil
	}

	logExp, err := otlploggrpc.New(ctx,
		otlploggrpc.WithEndpoint(target),
		otlploggrpc.WithInsecure(),
	)
	if err != nil {
		return stdoutLogger, shutdownTraceMetric, err
	}
	lp := sdklog.NewLoggerProvider(
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)),
		sdklog.WithResource(res),
	)
	otellog.SetLoggerProvider(lp)

	otelHandler := otelslog.NewHandler(serviceName, otelslog.WithLoggerProvider(lp))
	fanout := multiHandler{stdoutHandler, otelHandler}

	shutdown := func(c context.Context) error {
		_ = lp.Shutdown(c)
		_ = mp.Shutdown(c)
		return tp.Shutdown(c)
	}
	return slog.New(fanout), shutdown, nil
}

// multiHandler fans a slog.Record out to every embedded handler. Used to
// keep stdout writes (for `docker logs` / CloudWatch) alongside OTLP export.
type multiHandler []slog.Handler

func (m multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	var firstErr error
	for _, h := range m {
		if !h.Enabled(ctx, r.Level) {
			continue
		}
		if err := h.Handle(ctx, r); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (m multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithAttrs(attrs)
	}
	return out
}

func (m multiHandler) WithGroup(name string) slog.Handler {
	out := make(multiHandler, len(m))
	for i, h := range m {
		out[i] = h.WithGroup(name)
	}
	return out
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func stripScheme(s string) string {
	for _, p := range []string{"http://", "https://", "grpc://"} {
		if v, ok := strings.CutPrefix(s, p); ok {
			return v
		}
	}
	return s
}

// otelErrorHandler routes otel.Handle(err) through slog at error level. Bound
// to a stdout-only logger because the OTLP log exporter itself calls
// otel.Handle when it fails — routing those through the OTel log bridge would
// trigger another export, another failure, another error.
type otelErrorHandler struct{ logger *slog.Logger }

func (h otelErrorHandler) Handle(err error) {
	h.logger.LogAttrs(context.Background(), slog.LevelError, "otel sdk error", slog.Any("err", err))
}

// otelSlogHandler remaps the slog levels emitted by logr.FromSlogHandler to
// match the OTel SDK's verbosity convention (see
// go.opentelemetry.io/otel/internal/global): V(1)=warn, V(4)=info, V(8)=debug.
// logr.FromSlogHandler maps V(n).Info to slog.Level(-n), which would put OTel
// SDK warnings below LevelDebug and hide them entirely; the remap restores
// their intended severity for both Enabled() filtering and the JSON `level`
// field operators see.
type otelSlogHandler struct{ next slog.Handler }

func (h otelSlogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, remapVerbosityLevel(level))
}

func (h otelSlogHandler) Handle(ctx context.Context, r slog.Record) error {
	r.Level = remapVerbosityLevel(r.Level)
	return h.next.Handle(ctx, r)
}

func (h otelSlogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return otelSlogHandler{next: h.next.WithAttrs(attrs)}
}

func (h otelSlogHandler) WithGroup(name string) slog.Handler {
	return otelSlogHandler{next: h.next.WithGroup(name)}
}

func remapVerbosityLevel(in slog.Level) slog.Level {
	switch in {
	case -1:
		return slog.LevelWarn
	case -4:
		return slog.LevelInfo
	case -8:
		return slog.LevelDebug
	default:
		return in
	}
}
