// Package sqs is the SQS driver for the eventbus.Consumer port.
package sqs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus"
)

// Consumer drains one bound queue. Send/Purge/Attributes take an explicit URL
// so the same driver covers main+DLQ admin without re-binding.
type Consumer struct {
	c        *awssqs.Client
	queueURL string
}

// New binds the driver to one queue URL. queueURL may be "" when the caller
// only needs Send/Purge/Attributes against arbitrary URLs (DLQ admin).
func New(c *awssqs.Client, queueURL string) *Consumer {
	return &Consumer{c: c, queueURL: queueURL}
}

// QueueURL returns the bound URL.
func (c *Consumer) QueueURL() string { return c.queueURL }

func (c *Consumer) Receive(ctx context.Context, max int32, wait time.Duration) ([]eventbus.Message, error) {
	if c.queueURL == "" {
		return nil, errors.New("sqs: consumer not bound to a queue")
	}
	if max <= 0 {
		max = 1
	}
	waitSec := int32(wait.Seconds())
	if waitSec <= 0 {
		waitSec = 1
	}
	out, err := c.c.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
		QueueUrl:                    aws.String(c.queueURL),
		MaxNumberOfMessages:         max,
		WaitTimeSeconds:             waitSec,
		MessageAttributeNames:       []string{"All"},
		MessageSystemAttributeNames: []sqstypes.MessageSystemAttributeName{sqstypes.MessageSystemAttributeNameAll},
	})
	if err != nil {
		return nil, fmt.Errorf("sqs.Receive: %w", err)
	}
	return convertMessages(out.Messages, true), nil
}

// convertMessages maps SQS messages to eventbus.Message. When unwrap is true
// the body is passed through UnwrapSNS to strip the SNS Notification envelope.
func convertMessages(in []sqstypes.Message, unwrap bool) []eventbus.Message {
	msgs := make([]eventbus.Message, 0, len(in))
	for _, m := range in {
		msg := eventbus.Message{Attributes: map[string]string{}}
		if m.MessageId != nil {
			msg.ID = *m.MessageId
		}
		if m.ReceiptHandle != nil {
			msg.ReceiptHandle = *m.ReceiptHandle
		}
		if m.Body != nil {
			body := []byte(*m.Body)
			if unwrap {
				body = UnwrapSNS(body)
			}
			msg.Body = body
		}
		for k, v := range m.MessageAttributes {
			if v.StringValue != nil {
				msg.Attributes[k] = *v.StringValue
			}
		}
		msgs = append(msgs, msg)
	}
	return msgs
}

func (c *Consumer) Delete(ctx context.Context, handle string) error {
	if c.queueURL == "" {
		return errors.New("sqs: consumer not bound to a queue")
	}
	_, err := c.c.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.queueURL),
		ReceiptHandle: aws.String(handle),
	})
	return err
}

func (c *Consumer) ChangeVisibility(ctx context.Context, handle string, ttl time.Duration) error {
	if c.queueURL == "" {
		return errors.New("sqs: consumer not bound to a queue")
	}
	_, err := c.c.ChangeMessageVisibility(ctx, &awssqs.ChangeMessageVisibilityInput{
		QueueUrl:          aws.String(c.queueURL),
		ReceiptHandle:     aws.String(handle),
		VisibilityTimeout: int32(ttl.Seconds()),
	})
	return err
}

func (c *Consumer) Send(ctx context.Context, queueURL string, body []byte, attrs map[string]string) (string, error) {
	in := &awssqs.SendMessageInput{
		QueueUrl:    aws.String(queueURL),
		MessageBody: aws.String(string(body)),
	}
	if len(attrs) > 0 {
		sendAttrs := make(map[string]sqstypes.MessageAttributeValue, len(attrs))
		for k, v := range attrs {
			val := v
			sendAttrs[k] = sqstypes.MessageAttributeValue{DataType: aws.String("String"), StringValue: aws.String(val)}
		}
		in.MessageAttributes = sendAttrs
	}
	out, err := c.c.SendMessage(ctx, in)
	if err != nil {
		return "", err
	}
	if out.MessageId == nil {
		return "", nil
	}
	return *out.MessageId, nil
}

func (c *Consumer) Purge(ctx context.Context, queueURL string) error {
	_, err := c.c.PurgeQueue(ctx, &awssqs.PurgeQueueInput{QueueUrl: aws.String(queueURL)})
	return err
}

func (c *Consumer) Attributes(ctx context.Context, queueURL string) (map[string]string, error) {
	out, err := c.c.GetQueueAttributes(ctx, &awssqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
	})
	if err != nil {
		return nil, err
	}
	return out.Attributes, nil
}

// PeekDLQ receives messages from a DLQ with a fixed visibility so receipt
// handles stay valid for UI think-time. Caller passes the DLQ URL explicitly.
func (c *Consumer) PeekDLQ(ctx context.Context, queueURL string, max int32, visibility time.Duration) ([]eventbus.Message, error) {
	if max <= 0 {
		max = 10
	}
	if max > 10 {
		max = 10
	}
	out, err := c.c.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
		QueueUrl:                    aws.String(queueURL),
		MaxNumberOfMessages:         max,
		VisibilityTimeout:           int32(visibility.Seconds()),
		WaitTimeSeconds:             1,
		MessageAttributeNames:       []string{"All"},
		MessageSystemAttributeNames: []sqstypes.MessageSystemAttributeName{sqstypes.MessageSystemAttributeNameAll},
	})
	if err != nil {
		return nil, fmt.Errorf("sqs.PeekDLQ: %w", err)
	}
	return convertMessages(out.Messages, false), nil
}

// DeleteFromQueue acks a message receipt on an arbitrary queue (used by DLQ).
func (c *Consumer) DeleteFromQueue(ctx context.Context, queueURL, handle string) error {
	_, err := c.c.DeleteMessage(ctx, &awssqs.DeleteMessageInput{
		QueueUrl:      aws.String(queueURL),
		ReceiptHandle: aws.String(handle),
	})
	return err
}

// EnsureQueue creates the queue if absent. Idempotent.
func EnsureQueue(ctx context.Context, c *awssqs.Client, name string) (string, error) {
	if u, err := c.GetQueueUrl(ctx, &awssqs.GetQueueUrlInput{QueueName: aws.String(name)}); err == nil && u.QueueUrl != nil {
		return *u.QueueUrl, nil
	}
	out, err := c.CreateQueue(ctx, &awssqs.CreateQueueInput{QueueName: aws.String(name)})
	if err != nil {
		return "", fmt.Errorf("sqs.CreateQueue(%q): %w", name, err)
	}
	if out.QueueUrl == nil {
		return "", errors.New("sqs: nil queue url after create")
	}
	return *out.QueueUrl, nil
}

// UnwrapSNS strips the SNS Notification envelope (raw delivery disabled) and
// returns the inner Message bytes. Pass-through when the envelope is absent.
func UnwrapSNS(body []byte) []byte {
	var env struct {
		Type    string `json:"Type"`
		Message string `json:"Message"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Type == "Notification" && env.Message != "" {
		return []byte(env.Message)
	}
	return body
}
