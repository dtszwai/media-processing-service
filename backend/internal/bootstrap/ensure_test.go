package bootstrap

import (
	"encoding/json"
	"strings"
	"testing"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func TestGenFilterPolicyMatchesGenerationQueueNames(t *testing.T) {
	wantClasses := map[string]generation.ResourceClass{
		"fast":          generation.ResourceFast,
		"provider":      generation.ResourceProvider,
		"poll":          generation.ResourcePoll,
		"image-process": generation.ResourceImageProcess,
	}
	for _, queueName := range genapp.AllGenerationQueues() {
		policy := genFilterPolicy(queueName)
		if policy == "" {
			t.Fatalf("genFilterPolicy(%q) returned empty policy", queueName)
		}
		var got map[string][]string
		if err := json.Unmarshal([]byte(policy), &got); err != nil {
			t.Fatalf("genFilterPolicy(%q) returned invalid JSON %q: %v", queueName, policy, err)
		}
		parts := strings.SplitN(strings.TrimPrefix(queueName, "generation-jobs-"), "-", 2)
		if len(parts) != 2 {
			t.Fatalf("queue %q does not match generation-jobs-<tier>-<class>", queueName)
		}
		wantTier := strings.ToUpper(parts[0])
		wantClass := string(wantClasses[parts[1]])
		if wantClass == "" {
			t.Fatalf("queue %q has no expected resource class mapping", queueName)
		}
		if len(got[genapp.SNSAttrTier]) != 1 || got[genapp.SNSAttrTier][0] != wantTier {
			t.Fatalf("tier filter for %q = %#v want [%q]", queueName, got[genapp.SNSAttrTier], wantTier)
		}
		if len(got[genapp.SNSAttrResourceClass]) != 1 || got[genapp.SNSAttrResourceClass][0] != wantClass {
			t.Fatalf("resource_class filter for %q = %#v want [%q]", queueName, got[genapp.SNSAttrResourceClass], wantClass)
		}
	}
}

func TestGenFilterPolicyRejectsUnknownGenerationQueueSuffix(t *testing.T) {
	if got := genFilterPolicy("generation-jobs-free-image"); got != "" {
		t.Fatalf("genFilterPolicy accepted stale image suffix: %s", got)
	}
}

func TestApplyManagedTopologyPopulatesTerraformResources(t *testing.T) {
	genURLs := map[string]string{}
	for _, name := range genapp.AllGenerationQueues() {
		genURLs[name] = "http://sqs.local/" + name
	}
	rawGenURLs, err := json.Marshal(genURLs)
	if err != nil {
		t.Fatal(err)
	}

	aws := &AWS{GenerationQueues: map[string]string{}, DLQs: map[string]DLQInfo{}}
	managed, err := aws.applyManagedTopology(envTopology{
		mediaTopicARN:           "arn:media",
		mediaCleanupTopicARN:    "arn:cleanup",
		generationTopicARN:      "arn:generation",
		analyticsTopicARN:       "arn:analytics",
		mediaQueueURL:           "http://sqs.local/media",
		mediaCleanupQueueURL:    "http://sqs.local/cleanup",
		mediaUploadEventsURL:    "http://sqs.local/upload-events",
		webhookQueueURL:         "http://sqs.local/webhook",
		analyticsQueueURL:       "http://sqs.local/analytics",
		generationQueueURLsJSON: string(rawGenURLs),
	})
	if err != nil {
		t.Fatalf("applyManagedTopology: %v", err)
	}
	if !managed {
		t.Fatal("expected managed topology")
	}
	if aws.MediaTopicARN != "arn:media" || aws.MediaQueueURL != "http://sqs.local/media" {
		t.Fatalf("media topology not populated: topic=%q queue=%q", aws.MediaTopicARN, aws.MediaQueueURL)
	}
	if aws.MediaCleanupTopicARN != "arn:cleanup" || aws.MediaCleanupQueueURL != "http://sqs.local/cleanup" {
		t.Fatalf("cleanup topology not populated: topic=%q queue=%q", aws.MediaCleanupTopicARN, aws.MediaCleanupQueueURL)
	}
	if aws.GenerationTopicARN != "arn:generation" || len(aws.GenerationQueues) != len(genapp.AllGenerationQueues()) {
		t.Fatalf("generation topology not populated: topic=%q queues=%d", aws.GenerationTopicARN, len(aws.GenerationQueues))
	}
	if got := aws.GenerationQueues["generation-jobs-free-fast"]; got != "http://sqs.local/generation-jobs-free-fast" {
		t.Fatalf("generation-jobs-free-fast url = %q", got)
	}
}

func TestApplyManagedTopologyRejectsPartialTerraformEnv(t *testing.T) {
	aws := &AWS{GenerationQueues: map[string]string{}}
	managed, err := aws.applyManagedTopology(envTopology{mediaTopicARN: "arn:media"})
	if err == nil {
		t.Fatal("expected error for partial managed topology")
	}
	if !managed {
		t.Fatal("expected partial managed env to be detected")
	}
	if !strings.Contains(err.Error(), "managed topology incomplete") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseManagedGenerationQueueURLsAcceptsTerraformOutputKeys(t *testing.T) {
	got, err := parseManagedGenerationQueueURLs(`{"free-fast":"http://sqs.local/free-fast"}`)
	if err != nil {
		t.Fatalf("parseManagedGenerationQueueURLs: %v", err)
	}
	if got["generation-jobs-free-fast"] != "http://sqs.local/free-fast" {
		t.Fatalf("queue url map = %#v", got)
	}
}
