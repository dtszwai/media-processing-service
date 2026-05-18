// Package sns is the SNS driver for the eventbus.Publisher port.
package sns

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sns/types"
)

// Publisher publishes to a single topic ARN.
type Publisher struct {
	c        *sns.Client
	topicARN string
}

// New binds the driver to one topic.
func New(c *sns.Client, topicARN string) *Publisher {
	return &Publisher{c: c, topicARN: topicARN}
}

// Publish sends body with optional attrs to the bound topic.
func (p *Publisher) Publish(ctx context.Context, body []byte, attrs map[string]string) (string, error) {
	in := &sns.PublishInput{
		TopicArn: aws.String(p.topicARN),
		Message:  aws.String(string(body)),
	}
	if len(attrs) > 0 {
		in.MessageAttributes = toAttrs(attrs)
	}
	out, err := p.c.Publish(ctx, in)
	if err != nil {
		return "", fmt.Errorf("sns.Publish: %w", err)
	}
	return aws.ToString(out.MessageId), nil
}

func toAttrs(in map[string]string) map[string]types.MessageAttributeValue {
	out := make(map[string]types.MessageAttributeValue, len(in))
	for k, v := range in {
		out[k] = types.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(v),
		}
	}
	return out
}

// EnsureTopic creates the topic if absent and returns its ARN. Local/test only.
func EnsureTopic(ctx context.Context, c *sns.Client, name string) (string, error) {
	out, err := c.CreateTopic(ctx, &sns.CreateTopicInput{Name: aws.String(name)})
	if err != nil {
		return "", fmt.Errorf("sns.CreateTopic(%q): %w", name, err)
	}
	if out.TopicArn == nil {
		return "", errors.New("sns: nil topic arn")
	}
	return *out.TopicArn, nil
}

// Subscribe wires a queue ARN to a topic ARN with an optional JSON filter.
func Subscribe(ctx context.Context, c *sns.Client, topicARN, queueARN, filterPolicy string) (string, error) {
	attrs := map[string]string{}
	if filterPolicy != "" {
		attrs["FilterPolicy"] = filterPolicy
	}
	out, err := c.Subscribe(ctx, &sns.SubscribeInput{
		TopicArn:              aws.String(topicARN),
		Protocol:              aws.String("sqs"),
		Endpoint:              aws.String(queueARN),
		ReturnSubscriptionArn: true,
		Attributes:            attrs,
	})
	if err != nil {
		return "", fmt.Errorf("sns.Subscribe: %w", err)
	}
	return aws.ToString(out.SubscriptionArn), nil
}
