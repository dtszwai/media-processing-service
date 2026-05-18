// Package main is the lease-reaper scheduled Lambda. EventBridge Scheduler
// fires this every 5 minutes; it flips stale PENDING media rows to
// FAILED via media.Reaper.Run for each configured tenant and sweeps abandoned
// generation LEASE# rows.
//
// Dual-mode: AWS_LAMBDA_FUNCTION_NAME set → cron Lambda with warm-start
// bootstrap caching. Otherwise runs once and exits.
package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"golang.org/x/sync/errgroup"

	"github.com/dtszwai/media-processing-service/backend/cmd/internal/runtime"
	gendb "github.com/dtszwai/media-processing-service/backend/internal/app/generation/ddb"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
)

const serviceName = "lease-reaper"

// perTenantTimeout bounds how long a single tenant's reap scan may run so
// one slow scan can't starve the others on the same 5-minute cron window.
const perTenantTimeout = 2 * time.Minute

// maxTenantParallelism caps concurrent per-tenant Reaper.Run calls. Tenants
// share no DDB keys, so parallel scans are safe; the cap is chosen so a large
// tenant fleet doesn't burst DDB read capacity.
const maxTenantParallelism = 4

// awsResources is populated once on cold start and reused across warm Lambda
// invocations to amortise bootstrap cost across all subsequent calls.
var awsResources *bootstrap.AWS

// appCfg is loaded once on cold start. Env values don't change between
// invocations.
var appCfg app.Config

// Runner is the interface satisfied by *media.Reaper. Extracted so tests can
// inject a fake without touching DDB.
type Runner interface {
	Run(ctx context.Context, tenantID string) (int, error)
}

func main() {
	rt := runtime.Init(serviceName)
	defer rt.FlushTelemetry()
	appCfg = rt.Cfg

	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		lambda.Start(handleCron)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	res, err := bootstrap.Wire(ctx, appCfg)
	if err != nil {
		rt.Logger.ErrorContext(ctx, "bootstrap failed", "service", serviceName, "err", err)
		os.Exit(1)
	}

	reaper := mediaapp.NewReaper(res.KV, res.MediaRepo)
	if err := runOnce(ctx, reaper, appCfg.LeaseReaper.Tenants); err != nil {
		rt.Logger.ErrorContext(ctx, "reap failed", "service", serviceName, "err", err)
		os.Exit(2)
	}
	if err := sweepGenerationLeases(ctx, res, rt.Logger); err != nil {
		rt.Logger.ErrorContext(ctx, "generation lease sweep failed", "service", serviceName, "err", err)
		os.Exit(2)
	}
}

// handleCron is the Lambda entrypoint for EventBridge Scheduler invocations.
// Bootstrap runs once on cold start; warm invocations skip it.
func handleCron(ctx context.Context, _ events.CloudWatchEvent) error {
	if awsResources == nil {
		bootCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		res, err := bootstrap.Wire(bootCtx, appCfg)
		cancel()
		if err != nil {
			return err
		}
		awsResources = res
	}

	reaper := mediaapp.NewReaper(awsResources.KV, awsResources.MediaRepo)
	if err := runOnce(ctx, reaper, appCfg.LeaseReaper.Tenants); err != nil {
		return err
	}
	return sweepGenerationLeases(ctx, awsResources, slog.Default())
}

// sweepGenerationLeases runs one pass of the DDB lease sweep so abandoned
// LEASE# rows from crashed workers don't permanently starve their resource
// class.
func sweepGenerationLeases(ctx context.Context, res *bootstrap.AWS, logger *slog.Logger) error {
	if res == nil || res.KV == nil {
		return nil
	}
	lessor := gendb.NewResourceLessor(res.KV)
	sweepCtx, cancel := context.WithTimeout(ctx, perTenantTimeout)
	defer cancel()
	result, err := lessor.SweepExpired(sweepCtx)
	if err != nil {
		return err
	}
	logger.InfoContext(ctx, "generation lease sweep ok",
		"service", serviceName,
		"scanned", result.Scanned,
		"deleted", result.Deleted)
	return nil
}

// runOnce reaps every tenant concurrently with bounded parallelism. Each
// tenant gets its own deadline so one slow scan can't starve the others.
// Errors are aggregated so EventBridge surfaces failures upstream without
// aborting siblings.
func runOnce(ctx context.Context, r Runner, tenants []string) error {
	if len(tenants) == 0 {
		slog.WarnContext(ctx, "no tenants configured — nothing to reap; set lease_reaper.tenants",
			"service", serviceName)
		return nil
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(maxTenantParallelism)

	var mu sync.Mutex
	var errs []error

	for _, tenantID := range tenants {
		tenantID := tenantID
		g.Go(func() error {
			tenantCtx, cancel := context.WithTimeout(gctx, perTenantTimeout)
			defer cancel()
			flipped, err := r.Run(tenantCtx, tenantID)
			if err != nil {
				slog.ErrorContext(ctx, "reap failed",
					"service", serviceName, "tenant_id", tenantID,
					"flipped", flipped, "err", err)
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
				// nil so errgroup doesn't cancel siblings — aggregation owns the failure.
				return nil
			}
			slog.InfoContext(ctx, "reap ok",
				"service", serviceName, "tenant_id", tenantID, "flipped", flipped)
			return nil
		})
	}
	_ = g.Wait()
	return errors.Join(errs...)
}
