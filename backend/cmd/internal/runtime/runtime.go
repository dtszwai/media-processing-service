// Package runtime is the shared prologue for every cmd/* binary. It loads
// app config, installs the telemetry-aware slog default, and reports the
// service startup line. Workers should use RunWorker, which folds the
// prologue + bootstrap.Wire + signal handling + worker.Run dispatch into one
// call. Mains with a different shape (cron, api) call Init / SignalCtx
// directly.
package runtime

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/telemetry"
	"github.com/dtszwai/media-processing-service/backend/internal/worker"
)

// bootstrapTimeout bounds bootstrap.Wire on cold start. AWS API calls in the
// ensure-topology path can hang under packet loss; failing fast lets the
// container restart loop surface the problem.
const bootstrapTimeout = 60 * time.Second

// telemetryInitTimeout bounds OTLP exporter connection setup. Failure
// degrades to stdout logging; we don't block startup forever waiting for the
// collector.
const telemetryInitTimeout = 10 * time.Second

// shutdownTimeout bounds the deferred otel exporter flush.
const shutdownTimeout = 5 * time.Second

// Bootstrap carries the prologue result. Logger is always non-nil; Shutdown
// is always safe to call.
type Bootstrap struct {
	Cfg      app.Config
	Logger   *slog.Logger
	Shutdown func(context.Context) error
}

// Init runs the standard prologue: load config, init telemetry, install slog
// default. On app.Load failure the process exits 1.
func Init(name string) Bootstrap {
	cfg, err := app.Load()
	if err != nil {
		slog.Error("app config load failed", "service", name, "err", err)
		os.Exit(1)
	}
	otelCtx, cancel := context.WithTimeout(context.Background(), telemetryInitTimeout)
	defer cancel()
	logger, shutdown, terr := telemetry.Init(otelCtx, name, cfg)
	slog.SetDefault(logger)
	if terr != nil {
		logger.WarnContext(otelCtx, "otel init failed; continuing without traces", "service", name, "err", terr)
	}
	logger.InfoContext(otelCtx, "starting", "service", name)
	return Bootstrap{Cfg: cfg, Logger: logger, Shutdown: shutdown}
}

// FlushTelemetry runs Shutdown with a bounded timeout. Intended as `defer
// rt.FlushTelemetry()` in worker mains.
func (b Bootstrap) FlushTelemetry() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	_ = b.Shutdown(ctx)
}

// SignalCtx returns a context cancelled by SIGINT/SIGTERM and a stop function
// to call via defer.
func SignalCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
}

// RunWorker is the standard SQS-worker shape: prologue + bootstrap.Wire +
// signal-cancelled context + worker.Run. The setup callback receives the
// assembled AWS handle and returns the worker.Config (queue URL + handler).
// On any prologue failure the process exits 1.
func RunWorker(name string, setup func(ctx context.Context, rt Bootstrap, aws *bootstrap.AWS) worker.Config) {
	rt := Init(name)
	defer rt.FlushTelemetry()

	ctx, stop := SignalCtx()
	defer stop()

	bootCtx, cancel := context.WithTimeout(ctx, bootstrapTimeout)
	aws, err := bootstrap.Wire(bootCtx, rt.Cfg)
	cancel()
	if err != nil {
		rt.Logger.ErrorContext(ctx, "bootstrap", "err", err)
		os.Exit(1)
	}
	worker.Run(ctx, setup(ctx, rt, aws))
}
