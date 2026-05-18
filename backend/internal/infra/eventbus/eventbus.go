// Package eventbus is the pub/sub port. Drivers in impl/* bind a topic ARN or
// queue URL at construction so callers only ever see body + attrs.
package eventbus

import (
	"context"
	"time"
)

// Publisher publishes a body+attrs envelope to a pre-bound topic.
type Publisher interface {
	Publish(ctx context.Context, body []byte, attrs map[string]string) (msgID string, err error)
}

// Message is one SQS-style message envelope.
type Message struct {
	ID            string
	Body          []byte
	ReceiptHandle string
	Attributes    map[string]string
}

// Consumer drains a pre-bound queue and supports DLQ admin ops.
type Consumer interface {
	Receive(ctx context.Context, max int32, wait time.Duration) ([]Message, error)
	Delete(ctx context.Context, receiptHandle string) error
	ChangeVisibility(ctx context.Context, receiptHandle string, ttl time.Duration) error
	// Send pushes a message to an arbitrary queue URL (used by DLQ replay).
	Send(ctx context.Context, queueURL string, body []byte, attrs map[string]string) (msgID string, err error)
	Purge(ctx context.Context, queueURL string) error
	Attributes(ctx context.Context, queueURL string) (map[string]string, error)
}
