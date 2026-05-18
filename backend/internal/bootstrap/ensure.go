package bootstrap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	kmstypes "github.com/aws/aws-sdk-go-v2/service/kms/types"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"golang.org/x/sync/errgroup"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/conf/app"
	snsdrv "github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus/sns"
)

// applyManagedTopology binds Terraform-provided ARNs and URLs onto the
// bootstrap result. Presence of any managed value means the whole topology is
// expected to be present; partial env is rejected instead of being "fixed" by
// CreateTopic/CreateQueue calls that can drift from Terraform's subscriptions
// and queue policies.
func (a *AWS) applyManagedTopology(env envTopology) (bool, error) {
	if !env.hasManagedTopology() {
		return false, nil
	}

	generationURLs, err := parseManagedGenerationQueueURLs(env.generationQueueURLsJSON)
	if err != nil {
		return true, fmt.Errorf("bootstrap: managed topology: %w", err)
	}

	var missing []string
	for _, b := range env.bindings() {
		if !b.Required {
			continue
		}
		if strings.TrimSpace(*b.Field) == "" {
			missing = append(missing, b.Name)
		}
	}
	for _, name := range genapp.AllGenerationQueues() {
		if strings.TrimSpace(generationURLs[name]) == "" {
			missing = append(missing, "SQS_GENERATION_QUEUE_URLS."+name)
		}
	}
	if len(missing) > 0 {
		return true, fmt.Errorf("bootstrap: managed topology incomplete: missing %s", strings.Join(missing, ", "))
	}

	a.MediaTopicARN = env.mediaTopicARN
	a.MediaCleanupTopicARN = env.mediaCleanupTopicARN
	a.GenerationTopicARN = env.generationTopicARN
	a.AnalyticsTopicARN = env.analyticsTopicARN
	a.MediaQueueURL = env.mediaQueueURL
	a.MediaCleanupQueueURL = env.mediaCleanupQueueURL
	a.MediaUploadEventsQueueURL = env.mediaUploadEventsURL
	a.WebhookQueueURL = env.webhookQueueURL
	a.AnalyticsQueueURL = env.analyticsQueueURL
	a.GenerationQueues = generationURLs
	if a.DLQs == nil {
		a.DLQs = map[string]DLQInfo{}
	}
	return true, nil
}

func (e envTopology) hasManagedTopology() bool {
	for _, b := range e.bindings() {
		if !b.Required {
			continue
		}
		if strings.TrimSpace(*b.Field) != "" {
			return true
		}
	}
	return false
}

func parseManagedGenerationQueueURLs(raw string) (map[string]string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("decode SQS_GENERATION_QUEUE_URLS: %w", err)
	}
	out := make(map[string]string, len(decoded))
	for key, value := range decoded {
		name := key
		if !strings.HasPrefix(name, "generation-jobs-") {
			name = "generation-jobs-" + name
		}
		out[name] = value
	}
	return out, nil
}

// ensureTopology wires SNS topics, SQS queues, DLQs, and subscriptions. The
// resource names come straight from app.Config; ARNs and URLs are stored back
// on the receiver as they're discovered.
func (a *AWS) ensureTopology(ctx context.Context, awsCfg app.AWSConfig, snsClient *sns.Client, sqsClient *sqs.Client) error {
	type binding struct {
		topicName, queueName string
		topicARN, queueURL   *string
		filter               string
	}
	pubs := []binding{
		{awsCfg.Topics.Media, awsCfg.Queues.Media, &a.MediaTopicARN, &a.MediaQueueURL, ""},
		{awsCfg.Topics.MediaCleanup, awsCfg.Queues.MediaCleanup, &a.MediaCleanupTopicARN, &a.MediaCleanupQueueURL, ""},
		{awsCfg.Topics.Analytics, awsCfg.Queues.Analytics, &a.AnalyticsTopicARN, &a.AnalyticsQueueURL, ""},
	}
	for _, b := range pubs {
		if err := a.bindTopicToQueue(ctx, snsClient, sqsClient, b.topicName, b.queueName, b.filter, b.topicARN, b.queueURL); err != nil {
			return err
		}
	}

	// Standalone queue + DLQ (no SNS subscription).
	whURL, _, whDLQURL, err := ensureQueueWithDLQ(ctx, sqsClient, awsCfg.Queues.Webhook, defaultVisibilitySeconds)
	if err != nil {
		return fmt.Errorf("bootstrap: webhook queue+dlq: %w", err)
	}
	a.WebhookQueueURL = whURL
	a.DLQs["webhook-delivery-dlq"] = DLQInfo{Name: "webhook-delivery-dlq", URL: whDLQURL, SourceURL: whURL}

	// Standalone queue + DLQ — source is S3 ObjectCreated, not SNS. The
	// bucket-notification wiring lives in Terraform; ensureQueueWithDLQ here
	// just guarantees the queue exists when the local LocalStack stack boots
	// without `tflocal apply` (i.e. plain `make up` against `compose/local`).
	uploadURL, _, uploadDLQURL, err := ensureQueueWithDLQ(ctx, sqsClient, awsCfg.Queues.MediaUploadEvents, defaultVisibilitySeconds)
	if err != nil {
		return fmt.Errorf("bootstrap: media-upload-events queue+dlq: %w", err)
	}
	a.MediaUploadEventsQueueURL = uploadURL
	a.DLQs[awsCfg.Queues.MediaUploadEvents+"-dlq"] = DLQInfo{
		Name:      awsCfg.Queues.MediaUploadEvents + "-dlq",
		URL:       uploadDLQURL,
		SourceURL: uploadURL,
	}

	// Generation: one topic with 16 (tier × class) queues subscribed via filter.
	genTopicARN, err := snsdrv.EnsureTopic(ctx, snsClient, awsCfg.Topics.Generation)
	if err != nil {
		return fmt.Errorf("bootstrap: generation topic: %w", err)
	}
	a.GenerationTopicARN = genTopicARN
	return a.ensureGenerationQueues(ctx, snsClient, sqsClient, genTopicARN)
}

// bindTopicToQueue ensures (topic, queue, DLQ, subscription) for one pair and
// stores the resolved URLs/ARNs into the caller-provided pointers.
func (a *AWS) bindTopicToQueue(
	ctx context.Context,
	snsClient *sns.Client, sqsClient *sqs.Client,
	topicName, queueName, filter string,
	outTopicARN, outQueueURL *string,
) error {
	topicARN, err := snsdrv.EnsureTopic(ctx, snsClient, topicName)
	if err != nil {
		return fmt.Errorf("bootstrap: ensure topic %s: %w", topicName, err)
	}
	qURL, qARN, dlqURL, err := ensureQueueWithDLQ(ctx, sqsClient, queueName, defaultVisibilitySeconds)
	if err != nil {
		return fmt.Errorf("bootstrap: %s queue+dlq: %w", queueName, err)
	}
	if _, err := snsdrv.Subscribe(ctx, snsClient, topicARN, qARN, filter); err != nil {
		return fmt.Errorf("bootstrap: subscribe %s: %w", queueName, err)
	}
	*outTopicARN = topicARN
	*outQueueURL = qURL
	a.DLQs[queueName+"-dlq"] = DLQInfo{Name: queueName + "-dlq", URL: dlqURL, SourceURL: qURL}
	return nil
}

// ensureGenerationQueues creates the 16 (tier × class) queues + DLQs and
// subscribes each with its filter policy. Queues are independent, so the work
// runs concurrently with bounded parallelism.
func (a *AWS) ensureGenerationQueues(ctx context.Context, snsClient *sns.Client, sqsClient *sqs.Client, topicARN string) error {
	names := genapp.AllGenerationQueues()
	type result struct {
		name, url, dlqURL string
	}
	results := make([]result, len(names))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(8)
	for i, n := range names {
		i, name := i, n
		g.Go(func() error {
			qURL, qARN, dlqURL, err := ensureQueueWithDLQ(gctx, sqsClient, name, generationVisibilitySeconds)
			if err != nil {
				return fmt.Errorf("bootstrap: gen queue %s: %w", name, err)
			}
			if _, err := snsdrv.Subscribe(gctx, snsClient, topicARN, qARN, genFilterPolicy(name)); err != nil {
				return fmt.Errorf("bootstrap: subscribe %s: %w", name, err)
			}
			results[i] = result{name, qURL, dlqURL}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	for _, r := range results {
		a.GenerationQueues[r.name] = r.url
		a.DLQs[r.name+"-dlq"] = DLQInfo{Name: r.name + "-dlq", URL: r.dlqURL, SourceURL: r.url}
	}
	return nil
}

const localPromptKeyAlias = "alias/media-service-local-prompts"

func ensureLocalPromptKey(ctx context.Context, c *kms.Client) (string, error) {
	if _, err := c.DescribeKey(ctx, &kms.DescribeKeyInput{KeyId: aws.String(localPromptKeyAlias)}); err == nil {
		return localPromptKeyAlias, nil
	} else if !isKMSNotFound(err) {
		return "", err
	}
	key, err := c.CreateKey(ctx, &kms.CreateKeyInput{Description: aws.String("local media-service prompt envelope key")})
	if err != nil {
		return "", err
	}
	if key.KeyMetadata == nil || key.KeyMetadata.KeyId == nil {
		return "", fmt.Errorf("kms: create key returned no key id")
	}
	if _, err := c.CreateAlias(ctx, &kms.CreateAliasInput{
		AliasName:   aws.String(localPromptKeyAlias),
		TargetKeyId: key.KeyMetadata.KeyId,
	}); err != nil {
		var exists *kmstypes.AlreadyExistsException
		if !errors.As(err, &exists) {
			return "", err
		}
	}
	return localPromptKeyAlias, nil
}

// Generation queues run long stage handlers (audio overview, codex image)
// and must outlast their worst-case runtime; others run short handlers.
const (
	defaultVisibilitySeconds    = 120
	generationVisibilitySeconds = 1800
)

func ensureQueueWithDLQ(ctx context.Context, c *sqs.Client, name string, visibilitySeconds int) (queueURL, queueARN, dlqURL string, err error) {
	dlqOut, err := c.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name + "-dlq")})
	if err != nil {
		return "", "", "", err
	}
	dlqAttrs, err := c.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       dlqOut.QueueUrl,
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", "", "", err
	}
	dlqArn := dlqAttrs.Attributes[string(sqstypes.QueueAttributeNameQueueArn)]

	// CreateQueue rejects attr drift on existing queues, so create with no
	// attrs and apply them via SetQueueAttributes — config changes land on
	// the next boot without recreating the queue.
	qOut, err := c.CreateQueue(ctx, &sqs.CreateQueueInput{QueueName: aws.String(name)})
	if err != nil {
		return "", "", "", err
	}
	redrive := map[string]any{"deadLetterTargetArn": dlqArn, "maxReceiveCount": "5"}
	redriveJSON, _ := json.Marshal(redrive)
	if _, err := c.SetQueueAttributes(ctx, &sqs.SetQueueAttributesInput{
		QueueUrl: qOut.QueueUrl,
		Attributes: map[string]string{
			string(sqstypes.QueueAttributeNameRedrivePolicy):     string(redriveJSON),
			string(sqstypes.QueueAttributeNameVisibilityTimeout): strconv.Itoa(visibilitySeconds),
		},
	}); err != nil {
		return "", "", "", fmt.Errorf("bootstrap: set attrs on %s: %w", name, err)
	}
	qAttrs, err := c.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       qOut.QueueUrl,
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return "", "", "", err
	}
	return *qOut.QueueUrl, qAttrs.Attributes[string(sqstypes.QueueAttributeNameQueueArn)], *dlqOut.QueueUrl, nil
}

// genFilterPolicy returns the SNS subscription filter policy JSON for a
// generation queue. Queues are named "generation-jobs-<tier>-<class-suffix>";
// the SNS message attributes "tier" + "resource_class" must match this filter
// for the subscription to deliver the message.
func genFilterPolicy(queueName string) string {
	parts := strings.Split(queueName, "-")
	if len(parts) < 4 {
		return ""
	}
	tier := strings.ToUpper(parts[2])
	class := genapp.ResourceClassFromSuffix(strings.Join(parts[3:], "-"))
	if class == "" {
		return ""
	}
	b, _ := json.Marshal(map[string]any{
		genapp.SNSAttrTier:          []string{tier},
		genapp.SNSAttrResourceClass: []string{string(class)},
	})
	return string(b)
}

func isKMSNotFound(err error) bool {
	var nf *kmstypes.NotFoundException
	return errors.As(err, &nf)
}
