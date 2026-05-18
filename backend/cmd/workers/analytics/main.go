// Package main is the analytics-worker SQS consumer. Decodes events from the
// analytics-events SNS topic and applies them via the Sink to write sharded
// counters + active-tenant indices.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/cmd/internal/runtime"
	"github.com/dtszwai/media-processing-service/backend/internal/app/analytics"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	sqsdrv "github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus/sqs"
	"github.com/dtszwai/media-processing-service/backend/internal/worker"
)

const serviceName = "analytics-worker"

func main() {
	runtime.RunWorker(serviceName, func(_ context.Context, _ runtime.Bootstrap, res *bootstrap.AWS) worker.Config {
		return worker.Config{
			Service:     serviceName,
			QueueURL:    res.AnalyticsQueueURL,
			Consumer:    sqsdrv.New(res.SQS, res.AnalyticsQueueURL),
			MaxMessages: 10,
			PerMsgTTL:   30 * time.Second,
			Handler: func(ctx context.Context, body []byte) error {
				var evt analytics.Event
				if err := json.Unmarshal(sqsdrv.UnwrapSNS(body), &evt); err != nil {
					return fmt.Errorf("decode analytics event: %w", err)
				}
				return res.AnalyticsSink.Apply(ctx, evt)
			},
		}
	})
}
