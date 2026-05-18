package generation_test

import (
	"reflect"
	"strings"
	"testing"

	gen "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

func TestQueueName(t *testing.T) {
	cases := []struct {
		tier  generation.Tier
		class generation.ResourceClass
		want  string
	}{
		{generation.TierFree, generation.ResourceFast, "generation-jobs-free-fast"},
		{generation.TierPaid, generation.ResourceProvider, "generation-jobs-paid-provider"},
		{generation.TierFree, generation.ResourcePoll, "generation-jobs-free-poll"},
		{generation.TierFree, generation.ResourceImageProcess, "generation-jobs-free-image-process"},
	}
	for _, c := range cases {
		if got := gen.QueueName(c.tier, c.class); got != c.want {
			t.Fatalf("QueueName(%s,%s) = %q want %q", c.tier, c.class, got, c.want)
		}
	}
}

func TestQueueName_PanicsForUnknownResourceClass(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("QueueName did not panic for unknown resource class")
		}
	}()
	_ = gen.QueueName(generation.TierPaid, generation.ResourceClass("UNKNOWN"))
}

func TestIsFreeQueue(t *testing.T) {
	cases := []struct {
		tier  generation.Tier
		class generation.ResourceClass
	}{
		{generation.TierFree, generation.ResourceFast},
		{generation.TierFree, generation.ResourceImageProcess},
		{generation.TierPaid, generation.ResourceProvider},
		{generation.TierPaid, generation.ResourcePoll},
	}
	for _, c := range cases {
		name := gen.QueueName(c.tier, c.class)
		want := c.tier == generation.TierFree
		if got := gen.IsFreeQueue(name); got != want {
			t.Fatalf("IsFreeQueue(%q) = %v want %v", name, got, want)
		}
	}
	if gen.IsFreeQueue("media-jobs") {
		t.Fatal("IsFreeQueue must not match non-generation queues")
	}
}

func TestAllGenerationQueues_HasExpectedNames(t *testing.T) {
	queues := gen.AllGenerationQueues()
	want := []string{
		"generation-jobs-free-fast",
		"generation-jobs-free-provider",
		"generation-jobs-free-poll",
		"generation-jobs-free-image-process",
		"generation-jobs-paid-fast",
		"generation-jobs-paid-provider",
		"generation-jobs-paid-poll",
		"generation-jobs-paid-image-process",
	}
	if !reflect.DeepEqual(queues, want) {
		t.Fatalf("AllGenerationQueues() = %#v want %#v", queues, want)
	}
	seen := map[string]bool{}
	for _, q := range queues {
		if seen[q] {
			t.Fatalf("duplicate queue name: %s", q)
		}
		seen[q] = true
		if !strings.HasPrefix(q, "generation-jobs-") {
			t.Fatalf("queue %s missing canonical prefix", q)
		}
	}
}
