// Package testkit provides shared test helpers for in-memory fakes and live
// LocalStack integration tests.
package testkit

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	awscfg "github.com/dtszwai/media-processing-service/backend/internal/conf/aws"
)

const (
	defaultEndpoint = "http://localhost:4566"
)

// SkipIfIntegrationDisabled skips the test unless TEST_INTEGRATION=1 in the
// environment. Integration tests require a running LocalStack on :4566.
func SkipIfIntegrationDisabled(t *testing.T) {
	t.Helper()
	if os.Getenv("TEST_INTEGRATION") != "1" {
		t.Skip("skipping integration test; set TEST_INTEGRATION=1 to enable")
	}
}

// LocalstackAWSConfig returns an aws.Config pointed at LocalStack with the
// classic test/test credentials. Endpoint override comes from AWS_ENDPOINT_URL
// when set, otherwise http://localhost:4566.
func LocalstackAWSConfig(t *testing.T) aws.Config {
	t.Helper()
	endpoint := os.Getenv("AWS_ENDPOINT_URL")
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg, err := awscfg.Load(ctx, awscfg.Options{
		Region:      "us-east-1",
		EndpointURL: endpoint,
		AccessKey:   "test",
		SecretKey:   "test",
	})
	if err != nil {
		t.Fatalf("awscfg.Load: %v", err)
	}
	return cfg
}

// DDBClient returns a dynamodb.Client bound to LocalStack.
func DDBClient(t *testing.T) *dynamodb.Client {
	t.Helper()
	return dynamodb.NewFromConfig(LocalstackAWSConfig(t))
}

// S3Client returns an s3.Client bound to LocalStack with path-style addressing
// (required for LocalStack and custom endpoints).
func S3Client(t *testing.T) *s3.Client {
	t.Helper()
	return s3.NewFromConfig(LocalstackAWSConfig(t), func(o *s3.Options) {
		o.UsePathStyle = true
	})
}

// SQSClient returns an sqs.Client bound to LocalStack. Returned for use by
// driver constructors (e.g. infra/eventbus/sqs); app-layer tests should
// not call SDK methods on it directly.
func SQSClient(t *testing.T) *sqs.Client {
	t.Helper()
	return sqs.NewFromConfig(LocalstackAWSConfig(t))
}
