package generation

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// resourceClassSuffix is the canonical map between ResourceClass and the
// per-queue name suffix used by the infrastructure topology.
var resourceClassSuffix = [...]struct {
	class  generation.ResourceClass
	suffix string
}{
	{generation.ResourceFast, "fast"},
	{generation.ResourceProvider, "provider"},
	{generation.ResourcePoll, "poll"},
	{generation.ResourceImageProcess, "image-process"},
}

func tierPart(tier generation.Tier) string {
	if tier == generation.TierPaid {
		return "paid"
	}
	return "free"
}

// generationQueuePrefix is the canonical name prefix every per-tier ×
// resource-class generation queue carries. Single source of truth for
// QueueName, AllGenerationQueues, and IsFreeQueue.
const generationQueuePrefix = "generation-jobs-"

// QueueName returns the per-tier/resource-class generation queue name.
func QueueName(tier generation.Tier, class generation.ResourceClass) string {
	for _, m := range resourceClassSuffix {
		if m.class == class {
			return generationQueuePrefix + tierPart(tier) + "-" + m.suffix
		}
	}
	panic(fmt.Sprintf("unknown generation resource class %q", class))
}

// IsFreeQueue reports whether a generation queue name targets the Free tier.
// Mirrors the naming convention enforced by QueueName so consumers don't
// re-derive the prefix.
func IsFreeQueue(name string) bool {
	return strings.HasPrefix(name, generationQueuePrefix+tierPart(generation.TierFree)+"-")
}

// ResourceClassFromSuffix returns the class for a queue-name suffix (the part
// after "generation-jobs-<tier>-"). Returns "" when unknown so bootstrap can
// detect a missing filter mapping rather than silently subscribing on no
// attributes.
func ResourceClassFromSuffix(suffix string) generation.ResourceClass {
	for _, m := range resourceClassSuffix {
		if m.suffix == suffix {
			return m.class
		}
	}
	return ""
}

// SNS message-attribute keys for the `generation-jobs` topic. Load-bearing:
// the producer here and the SNS subscription filter policy in bootstrap must
// agree letter-for-letter, otherwise a fan-out arrives at zero subscribers.
const (
	SNSAttrTier          = "tier"
	SNSAttrStage         = "stage"
	SNSAttrResourceClass = "resource_class"
	SNSAttrTenantLane    = "tenant_lane"
)

func TenantLane(tenantID string) string {
	if tenantID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(tenantID))
	return "lane-" + hex.EncodeToString(sum[:2])
}

// AllGenerationQueues enumerates every (tier × resource_class) queue name.
// Used at bootstrap and by Terraform planning to enforce parity.
func AllGenerationQueues() []string {
	tiers := []generation.Tier{generation.TierFree, generation.TierPaid}
	out := make([]string, 0, len(tiers)*len(resourceClassSuffix))
	for _, t := range tiers {
		for _, m := range resourceClassSuffix {
			out = append(out, QueueName(t, m.class))
		}
	}
	return out
}
