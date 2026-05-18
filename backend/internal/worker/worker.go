// Package worker shares the SQS-driven dual-mode runtime used by every
// background binary: AWS_LAMBDA_FUNCTION_NAME → SQS-event Lambda; otherwise a
// long-poll loop with per-message timeout.
package worker

import (
	"context"
	"log/slog"
	"os"
	"time"

	lambdaevents "github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"

	sqsdrv "github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus/sqs"
)

// Handler processes one SQS message body. Implementations are expected to be
// idempotent — Lambda and the poll loop both redeliver on transient error.
type Handler func(ctx context.Context, body []byte) error

// Config wires one polling worker.
type Config struct {
	Service     string
	QueueURL    string
	Consumer    *sqsdrv.Consumer
	Handler     Handler
	MaxMessages int32
	WaitSeconds int32
	PerMsgTTL   time.Duration
}

func (c *Config) withDefaults() {
	if c.MaxMessages <= 0 {
		c.MaxMessages = 5
	}
	if c.WaitSeconds <= 0 {
		c.WaitSeconds = 5
	}
	if c.PerMsgTTL <= 0 {
		c.PerMsgTTL = 60 * time.Second
	}
}

// Run starts the worker. In Lambda mode it returns only on cold-stop; in poll
// mode it returns when ctx is cancelled.
func Run(ctx context.Context, c Config) {
	c.withDefaults()
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		lambda.Start(lambdaHandler(c))
		return
	}
	pollLoop(ctx, c)
}

func lambdaHandler(c Config) func(ctx context.Context, evt lambdaevents.SQSEvent) (lambdaevents.SQSEventResponse, error) {
	return func(ctx context.Context, evt lambdaevents.SQSEvent) (lambdaevents.SQSEventResponse, error) {
		var resp lambdaevents.SQSEventResponse
		for _, rec := range evt.Records {
			if err := c.Handler(ctx, sqsdrv.UnwrapSNS([]byte(rec.Body))); err != nil {
				slog.ErrorContext(ctx, "worker handler failed", "service", c.Service, "err", err, "msg_id", rec.MessageId)
				resp.BatchItemFailures = append(resp.BatchItemFailures, lambdaevents.SQSBatchItemFailure{ItemIdentifier: rec.MessageId})
			}
		}
		return resp, nil
	}
}

func pollLoop(ctx context.Context, c Config) {
	if c.QueueURL == "" {
		slog.ErrorContext(ctx, "worker: queue URL not set", "service", c.Service)
		return
	}
	slog.InfoContext(ctx, "polling", "service", c.Service, "queue", c.QueueURL)
	for {
		if ctx.Err() != nil {
			return
		}
		msgs, err := c.Consumer.Receive(ctx, c.MaxMessages, time.Duration(c.WaitSeconds)*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "sqs receive", "service", c.Service, "err", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		for _, m := range msgs {
			handleCtx, cancel := context.WithTimeout(ctx, c.PerMsgTTL)
			if err := c.Handler(handleCtx, m.Body); err != nil {
				slog.ErrorContext(handleCtx, "worker handler failed", "service", c.Service, "err", err, "id", m.ID)
				cancel()
				continue
			}
			cancel()
			if err := c.Consumer.Delete(ctx, m.ReceiptHandle); err != nil {
				slog.WarnContext(ctx, "sqs delete", "service", c.Service, "err", err)
			}
		}
	}
}
