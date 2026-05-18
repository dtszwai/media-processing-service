// Package main is the webhook-worker SQS consumer. POSTs HMAC-SHA256-
// signed bodies to customer URLs with bounded retry + idempotency.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/cmd/internal/runtime"
	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	"github.com/dtszwai/media-processing-service/backend/internal/app/webhook"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	sqsdrv "github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus/sqs"
	"github.com/dtszwai/media-processing-service/backend/internal/worker"
)

const serviceName = "webhook-worker"

func main() {
	runtime.RunWorker(serviceName, func(_ context.Context, _ runtime.Bootstrap, res *bootstrap.AWS) worker.Config {
		dispatcher := webhook.NewDispatcher(res.WebhookSecret)
		dispatcher.Idempotency = res.Idempotency
		dispatcher.Secrets = res.WebhookSecrets
		dispatcher.Instruments = res.Instruments
		return worker.Config{
			Service:   serviceName,
			QueueURL:  res.WebhookQueueURL,
			Consumer:  sqsdrv.New(res.SQS, res.WebhookQueueURL),
			PerMsgTTL: 60 * time.Second,
			Handler: func(ctx context.Context, body []byte) error {
				var env events.WebhookDeliveryEnvelope
				if err := json.Unmarshal(body, &env); err != nil {
					return fmt.Errorf("decode envelope: %w", err)
				}
				return dispatcher.Send(ctx, env)
			},
		}
	})
}
