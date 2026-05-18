// Package main is the media-worker SQS consumer. Drains media-jobs and invokes
// the derive handler.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/cmd/internal/runtime"
	"github.com/dtszwai/media-processing-service/backend/internal/app/derive"
	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	sqsdrv "github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus/sqs"
	"github.com/dtszwai/media-processing-service/backend/internal/worker"
)

const serviceName = "media-worker"

func main() {
	runtime.RunWorker(serviceName, func(_ context.Context, _ runtime.Bootstrap, res *bootstrap.AWS) worker.Config {
		consumer := sqsdrv.New(res.SQS, res.MediaQueueURL)
		enq := &webhookEnqueuer{transport: consumer, queueURL: res.WebhookQueueURL}
		handler := derive.NewHandler(res.MediaRepo, res.Blob, enq)
		return worker.Config{
			Service:   serviceName,
			QueueURL:  res.MediaQueueURL,
			Consumer:  consumer,
			PerMsgTTL: 120 * time.Second,
			Handler: func(ctx context.Context, body []byte) error {
				var evt events.MediaEvent
				if err := json.Unmarshal(body, &evt); err != nil {
					return fmt.Errorf("decode media event: %w", err)
				}
				return handler.HandleEvent(ctx, evt)
			},
		}
	})
}

type webhookEnqueuer struct {
	transport *sqsdrv.Consumer
	queueURL  string
}

func (s *webhookEnqueuer) EnqueueWebhook(ctx context.Context, env events.WebhookDeliveryEnvelope) error {
	body, err := json.Marshal(env)
	if err != nil {
		return err
	}
	_, err = s.transport.Send(ctx, s.queueURL, body, nil)
	return err
}
