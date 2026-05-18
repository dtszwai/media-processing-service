// Package main is the generation-worker SQS consumer. It drains the per-tier
// × resource-class generation queues and runs one stage per message.
//
// Dual-mode: AWS_LAMBDA_FUNCTION_NAME set → Lambda. Otherwise poll loop.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awssqs "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/dtszwai/media-processing-service/backend/cmd/internal/runtime"
	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
	sqsdrv "github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus/sqs"
)

const serviceName = "generation-worker"

// appCfg is populated on cold start and read by every handler invocation.
var appCfg app.Config

// workerOnce caches stageWorker construction across warm Lambda invocations.
// bootstrap.Wire is the expensive part — DDB describe, KMS, S3 head, secrets
// — and the result is invocation-independent.
var (
	workerOnce sync.Once
	cached     *stageWorker
	cachedErr  error
)

type stageWorker struct {
	runner      *genapp.StageRunner
	dlqConsumer *genapp.DLQConsumer
	sqs         *awssqs.Client
	queues      map[string]string
	dlqs        map[string]string
}

func main() {
	rt := runtime.Init(serviceName)
	defer rt.FlushTelemetry()
	appCfg = rt.Cfg

	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		lambda.Start(handleSQSEvent)
		return
	}

	ctx, stop := runtime.SignalCtx()
	defer stop()
	worker, err := getStageWorker(ctx)
	if err != nil {
		rt.Logger.ErrorContext(ctx, "generation worker init failed", "err", err)
		os.Exit(1)
	}
	runLongLived(ctx, worker, rt.Logger)
	rt.Logger.InfoContext(ctx, "shutdown", "service", serviceName)
}

func handleSQSEvent(ctx context.Context, evt events.SQSEvent) (events.SQSEventResponse, error) {
	worker, err := getStageWorker(ctx)
	if err != nil {
		return events.SQSEventResponse{}, err
	}
	resp := events.SQSEventResponse{}
	for _, record := range evt.Records {
		process := worker.processBody
		if isGenerationDLQARN(record.EventSourceARN) {
			process = worker.processDLQBody
		}
		if err := process(ctx, []byte(record.Body)); err != nil {
			slog.ErrorContext(ctx, "generation stage failed", "message_id", record.MessageId, "err", err)
			resp.BatchItemFailures = append(resp.BatchItemFailures, events.SQSBatchItemFailure{ItemIdentifier: record.MessageId})
		}
	}
	return resp, nil
}

func getStageWorker(ctx context.Context) (*stageWorker, error) {
	workerOnce.Do(func() {
		cached, cachedErr = newStageWorker(ctx)
	})
	return cached, cachedErr
}

func newStageWorker(ctx context.Context) (*stageWorker, error) {
	bootCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	res, err := bootstrap.Wire(bootCtx, appCfg)
	if err != nil {
		return nil, err
	}
	pickers, err := bootstrap.NewProviderRegistry(appCfg.Generation)
	if err != nil {
		return nil, fmt.Errorf("generation worker: provider registry: %w", err)
	}
	runner := &genapp.StageRunner{
		Repo:          res.JobRepo,
		Idem:          res.Idempotency,
		Sink:          res.ArtifactSink,
		Stager:        res.StagedArtifact,
		ImageStamper:  res.ImageStamper,
		LeaseRunner:   res.LeaseRunner,
		Quota:         res.QuotaAdapter,
		Ledger:        res.QuotaAdapter,
		Sealer:        res.Sealer,
		Pickers:       pickers,
		Moderator:     res.Moderator,
		AuditRecorder: res.AuditRecorder,
		UsageMeter:    res.QuotaMeter,
		Instruments:   res.Instruments,
	}
	return &stageWorker{
		runner:      runner,
		dlqConsumer: &genapp.DLQConsumer{Repo: res.JobRepo, Attempts: res.JobRepo, Ledger: res.QuotaAdapter},
		sqs:         res.SQS,
		queues:      res.GenerationQueues,
		dlqs:        generationDLQURLs(res.DLQs),
	}, nil
}

func (w *stageWorker) processBody(ctx context.Context, body []byte) error {
	return w.runner.ProcessMessage(ctx, sqsdrv.UnwrapSNS(body))
}

func (w *stageWorker) processDLQBody(ctx context.Context, body []byte) error {
	return w.dlqConsumer.ProcessMessage(ctx, sqsdrv.UnwrapSNS(body))
}

// sqsMaxBatchSize is the SQS ReceiveMessage batch ceiling. Reused by both
// the Paid live-queue policy and the DLQ poller — they share the API limit,
// not a tuning knob.
const sqsMaxBatchSize int32 = 10

// paidQueuePollers is the per-Paid-queue parallelism. Bumping it raises
// throughput linearly until DDB / provider throttling becomes the bottleneck.
const paidQueuePollers = 8

// pollPolicy decides how many concurrent pollers run against a queue and how
// many messages each Receive pulls. Free queues run a single serial poller so
// the Free path delivers backpressure-by-infra; Paid queues fan out.
type pollPolicy struct {
	pollers     int
	maxMessages int32
}

func policyForGenerationQueue(name string) pollPolicy {
	if genapp.IsFreeQueue(name) {
		return pollPolicy{pollers: 1, maxMessages: 1}
	}
	return pollPolicy{pollers: paidQueuePollers, maxMessages: sqsMaxBatchSize}
}

// runLongLived fans out pollers per generation queue under the per-queue
// policyForGenerationQueue. DLQ queues stay uniform — DLQ replay is admin, not
// hot-path. Each receive uses a 10s long-poll so a sequential sweep would
// block all queues behind the slowest one.
func runLongLived(ctx context.Context, worker *stageWorker, logger *slog.Logger) {
	var wg sync.WaitGroup
	for name, url := range worker.queues {
		policy := policyForGenerationQueue(name)
		for i := 0; i < policy.pollers; i++ {
			worker.runPoller(ctx, &wg, name, url, policy.maxMessages, worker.processBody, "generation queue poll failed", logger)
		}
	}
	for name, url := range worker.dlqs {
		worker.runPoller(ctx, &wg, name, url, sqsMaxBatchSize, worker.processDLQBody, "generation dlq poll failed", logger)
	}
	wg.Wait()
}

// runPoller spawns one polling goroutine that drains queueURL until ctx is
// cancelled. Used for both live queues and DLQs — they differ only in
// handler and log tag.
func (w *stageWorker) runPoller(ctx context.Context, wg *sync.WaitGroup, queueName, queueURL string, maxMsgs int32, handler func(context.Context, []byte) error, logTag string, logger *slog.Logger) {
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			if _, err := w.drain(ctx, queueURL, maxMsgs, handler); err != nil && ctx.Err() == nil {
				logger.ErrorContext(ctx, logTag, "queue", queueName, "err", err)
				// brief backoff so a wedged queue doesn't tight-loop
				select {
				case <-ctx.Done():
					return
				case <-time.After(time.Second):
				}
			}
		}
	}()
}

// drain receives a batch from queueURL, runs fn against each message body, and
// deletes the messages that fn handled successfully. Used for both live
// generation queues and their DLQs — they differ only in the per-message
// handler.
func (w *stageWorker) drain(ctx context.Context, queueURL string, maxMessages int32, fn func(context.Context, []byte) error) (int, error) {
	out, err := w.sqs.ReceiveMessage(ctx, &awssqs.ReceiveMessageInput{
		QueueUrl:            aws.String(queueURL),
		MaxNumberOfMessages: maxMessages,
		WaitTimeSeconds:     10,
	})
	if err != nil {
		return 0, err
	}
	deletes := make([]sqstypes.DeleteMessageBatchRequestEntry, 0, len(out.Messages))
	for _, msg := range out.Messages {
		if err := fn(ctx, []byte(aws.ToString(msg.Body))); err != nil {
			slog.ErrorContext(ctx, "generation stage failed", "message_id", aws.ToString(msg.MessageId), "err", err)
			continue
		}
		deletes = append(deletes, sqstypes.DeleteMessageBatchRequestEntry{
			Id:            msg.MessageId,
			ReceiptHandle: msg.ReceiptHandle,
		})
	}
	if len(deletes) > 0 {
		deleteOut, err := w.sqs.DeleteMessageBatch(ctx, &awssqs.DeleteMessageBatchInput{
			QueueUrl: aws.String(queueURL),
			Entries:  deletes,
		})
		if err != nil {
			return 0, err
		}
		if deleteOut != nil && len(deleteOut.Failed) > 0 {
			return 0, fmt.Errorf("generation worker: delete message batch partial failure: %s", formatBatchFailures(deleteOut.Failed))
		}
	}
	return len(out.Messages), nil
}

func formatBatchFailures(failed []sqstypes.BatchResultErrorEntry) string {
	parts := make([]string, 0, len(failed))
	for _, f := range failed {
		parts = append(parts, fmt.Sprintf("%s:%s:%s", aws.ToString(f.Id), aws.ToString(f.Code), aws.ToString(f.Message)))
	}
	return strings.Join(parts, ", ")
}

func generationDLQURLs(dlqs map[string]bootstrap.DLQInfo) map[string]string {
	out := map[string]string{}
	for name, info := range dlqs {
		if strings.HasPrefix(name, "generation-jobs-") && strings.HasSuffix(name, "-dlq") && info.URL != "" {
			out[name] = info.URL
		}
	}
	return out
}

func isGenerationDLQARN(arn string) bool {
	parts := strings.Split(arn, ":")
	if len(parts) == 0 {
		return false
	}
	name := parts[len(parts)-1]
	return strings.HasPrefix(name, "generation-jobs-") && strings.HasSuffix(name, "-dlq")
}
