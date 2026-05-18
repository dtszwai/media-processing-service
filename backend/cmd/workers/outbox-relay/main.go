// Package main is the outbox-relay worker. It drains the transactional-outbox
// partitions for the MEDIA, MEDIA_CLEANUP, and GEN streams; derives SNS
// message attributes via a RoutingPolicy validated against the allowed enums;
// and publishes to the per-stream SNS topic. Failed routing or exhausted
// retries route the row to OUTBOX_DLQ#<stream>#<day> for operator replay.
//
// Two modes, selected by AWS_LAMBDA_FUNCTION_NAME:
//   - Lambda: EventBridge Scheduler ticks the function every minute; one tick
//     drains every (stream, shard) pair once and exits.
//   - Long-poll: cmd binary runs a forever loop with a per-shard goroutine,
//     used by the compose stack so make up exercises the same code path.
//
// The relay intentionally lives in its own binary. Co-locating it inside the
// API process would couple media + cleanup + generation stream draining to
// API health — a single API restart would stall every downstream consumer.
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
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus"
)

const serviceName = "outbox-relay"

// stepInterval is the cadence at which long-poll mode steps each shard. Short
// enough that operator-visible relay latency stays low without burning DDB
// read capacity polling empty partitions.
const stepInterval = 2 * time.Second

// shardsPerStream is the number of partition shards each stream is fanned out
// across. The relay creates one goroutine per (stream, shard) tuple; 8 shards
// times 3 streams = 24 goroutines, which is well under any reasonable
// per-process limit. Must match the producer's PK() shard count.
const shardsPerStream = 8

// stepTimeout bounds one Step() invocation. Generous because the underlying
// Query/Update/Publish/Delete fan-out can take a few seconds under load; the
// goal is just to prevent a stuck call from blocking the next tick forever.
const stepTimeout = 30 * time.Second

// awsResources is populated on cold start and reused across warm Lambda
// invocations. Bootstrap cost is amortised across every subsequent tick.
var awsResources *bootstrap.AWS

// appCfg is loaded once on cold start; env values do not change between
// invocations.
var appCfg app.Config

func main() {
	rt := runtime.Init(serviceName)
	defer rt.FlushTelemetry()
	appCfg = rt.Cfg

	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		lambda.Start(handleCron)
		return
	}

	ctx, stop := runtime.SignalCtx()
	defer stop()

	bootCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	res, err := bootstrap.Wire(bootCtx, appCfg)
	cancel()
	if err != nil {
		rt.Logger.ErrorContext(ctx, "bootstrap failed", "service", serviceName, "err", err)
		os.Exit(1)
	}

	relays, err := buildRelays(res)
	if err != nil {
		rt.Logger.ErrorContext(ctx, "build relays failed", "service", serviceName, "err", err)
		os.Exit(1)
	}
	if len(relays) == 0 {
		rt.Logger.WarnContext(ctx, "no outbox streams configured — relay idle", "service", serviceName)
	}

	runLongPoll(ctx, rt.Logger, relays)
}

// handleCron is the Lambda entrypoint for EventBridge Scheduler invocations.
// One tick drains every shard once across every stream and returns; the next
// tick picks up wherever the checkpoint left off. Failures on one
// (stream, shard) pair don't abort the others — error aggregation lets the
// Scheduler retry policy decide.
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
	relays, err := buildRelays(awsResources)
	if err != nil {
		return err
	}
	return runOnce(ctx, slog.Default(), relays)
}

// streamRelay binds a relay to its stream so the long-poll loop can log the
// stream name with each step failure.
type streamRelay struct {
	Stream string
	Relay  *outbox.Relay
}

// buildRelays wires one Relay per stream against its bound publisher. A
// stream with no configured publisher is skipped — that's the local-dev
// shape where the operator hasn't provisioned every topic.
func buildRelays(res *bootstrap.AWS) ([]streamRelay, error) {
	if res == nil || res.KV == nil {
		return nil, errors.New("outbox-relay: bootstrap result missing KV driver")
	}
	publishers := map[string]eventbus.Publisher{
		outbox.StreamMedia:        res.MediaPub,
		outbox.StreamMediaCleanup: res.MediaCleanupPub,
		outbox.StreamGeneration:   res.GenerationPub,
	}
	out := make([]streamRelay, 0, len(outbox.AllStreams))
	for _, stream := range outbox.AllStreams {
		pub := publishers[stream]
		if pub == nil {
			continue
		}
		out = append(out, streamRelay{
			Stream: stream,
			Relay:  outbox.NewRelay(res.KV, stream, shardsPerStream, pub).WithInstruments(res.Instruments),
		})
	}
	return out, nil
}

// runOnce drains every (stream, shard) pair exactly once. Used by the Lambda
// entrypoint where each invocation is bounded by the Scheduler's cadence.
func runOnce(ctx context.Context, logger *slog.Logger, relays []streamRelay) error {
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(len(relays) * shardsPerStream)
	for _, sr := range relays {
		sr := sr
		for shard := 0; shard < sr.Relay.Shards; shard++ {
			shard := shard
			g.Go(func() error {
				stepCtx, cancel := context.WithTimeout(gctx, stepTimeout)
				defer cancel()
				if _, err := sr.Relay.Step(stepCtx, shard); err != nil {
					logger.ErrorContext(ctx, "outbox relay step failed",
						"service", serviceName, "stream", sr.Stream, "shard", shard, "err", err)
					return err
				}
				return nil
			})
		}
	}
	return g.Wait()
}

// runLongPoll spawns one goroutine per (stream, shard) tuple and steps each
// on the configured interval until ctx is cancelled. Used by the compose
// stack so make up exercises the same code path as Lambda without the
// invocation cadence.
func runLongPoll(ctx context.Context, logger *slog.Logger, relays []streamRelay) {
	var wg sync.WaitGroup
	for _, sr := range relays {
		for shard := 0; shard < sr.Relay.Shards; shard++ {
			wg.Add(1)
			go func(sr streamRelay, shard int) {
				defer wg.Done()
				ticker := time.NewTicker(stepInterval)
				defer ticker.Stop()
				for {
					select {
					case <-ctx.Done():
						return
					case <-ticker.C:
						stepCtx, cancel := context.WithTimeout(ctx, stepTimeout)
						if _, err := sr.Relay.Step(stepCtx, shard); err != nil && ctx.Err() == nil {
							logger.ErrorContext(ctx, "outbox relay step failed",
								"service", serviceName, "stream", sr.Stream, "shard", shard, "err", err)
						}
						cancel()
					}
				}
			}(sr, shard)
		}
	}
	wg.Wait()
}
