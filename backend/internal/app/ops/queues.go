package ops

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

// QueueStat is one row in the queues tab.
type QueueStat struct {
	Name                  string
	URL                   string
	Visible               int32
	InFlight              int32
	Delayed               int32
	VisibilityTimeoutSecs int32
	OldestMessageAgeSecs  int32
	DLQName               string
	DLQCount              int32
	TierClass             string
}

// QueueDepths fans out one GetQueueAttributes call per known queue. SQS
// rate limits this at ~300 req/sec/account in real AWS; for LocalStack at
// console scale (a dozen queues) the fan-out is fine.
func (s *Service) QueueDepths(ctx context.Context) ([]QueueStat, error) {
	if s.SQS == nil {
		return nil, fmt.Errorf("ops: sqs client not wired")
	}
	stats := make([]QueueStat, 0, len(s.QueueURLs)+len(s.DLQs))
	for name, url := range s.QueueURLs {
		stat, err := s.queueStat(ctx, name, url)
		if err != nil {
			continue
		}
		stat.TierClass = tierClassFor(name)
		stats = append(stats, stat)
	}
	for name, info := range s.DLQs {
		stat, err := s.queueStat(ctx, name, info.URL)
		if err != nil {
			continue
		}
		stat.DLQName = name
		stats = append(stats, stat)
	}
	// Compose source-queue DLQ rollups: when a primary queue names a DLQ
	// via its RedrivePolicy, attach the DLQ's depth here so the operator
	// sees the failure count alongside the live depth.
	dlqIndex := map[string]int32{}
	for _, st := range stats {
		if st.DLQName != "" {
			dlqIndex[st.DLQName] = st.Visible
		}
	}
	// Only attach a primary→DLQ rollup when the DLQ actually exists in
	// our topology — naming-by-convention would surface phantom DLQs for
	// every queue that doesn't have one.
	for i := range stats {
		if stats[i].DLQName != "" {
			continue
		}
		dlqName := stats[i].Name + "-dlq"
		if _, ok := s.DLQs[dlqName]; ok {
			stats[i].DLQName = dlqName
			stats[i].DLQCount = dlqIndex[dlqName]
		}
	}
	sort.Slice(stats, func(i, j int) bool {
		// DLQs sort to the bottom; primary queues stay alphabetical.
		if (stats[i].DLQName != "" && stats[j].DLQName == "") || (stats[j].DLQName != "" && stats[i].DLQName == "") {
			return stats[i].DLQName == ""
		}
		return stats[i].Name < stats[j].Name
	})
	return stats, nil
}

func (s *Service) queueStat(ctx context.Context, name, url string) (QueueStat, error) {
	if url == "" {
		return QueueStat{}, fmt.Errorf("ops: queue %q url empty", name)
	}
	out, err := s.SQS.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(url),
		AttributeNames: []types.QueueAttributeName{
			types.QueueAttributeNameApproximateNumberOfMessages,
			types.QueueAttributeNameApproximateNumberOfMessagesNotVisible,
			types.QueueAttributeNameApproximateNumberOfMessagesDelayed,
			types.QueueAttributeNameVisibilityTimeout,
			types.QueueAttributeNameRedrivePolicy,
		},
	})
	if err != nil {
		return QueueStat{}, err
	}
	stat := QueueStat{Name: name, URL: url}
	if v, ok := out.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessages)]; ok {
		stat.Visible = parseInt32(v)
	}
	if v, ok := out.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessagesNotVisible)]; ok {
		stat.InFlight = parseInt32(v)
	}
	if v, ok := out.Attributes[string(types.QueueAttributeNameApproximateNumberOfMessagesDelayed)]; ok {
		stat.Delayed = parseInt32(v)
	}
	if v, ok := out.Attributes[string(types.QueueAttributeNameVisibilityTimeout)]; ok {
		stat.VisibilityTimeoutSecs = parseInt32(v)
	}
	// Oldest message age requires a separate ReceiveMessage with peek;
	// LocalStack's ApproximateAgeOfOldestMessage attribute is unreliable
	// so the console accepts an empty field rather than show a wrong number.
	return stat, nil
}

func parseInt32(s string) int32 {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return int32(n)
}

// tierClassFor extracts the tier+resource_class projection from a generation
// queue name. The naming convention is `generation-jobs-<tier>-<class>` per
// terraform/modules/sns-sqs; anything else falls back to "".
func tierClassFor(name string) string {
	if !strings.HasPrefix(name, "generation-jobs-") {
		return ""
	}
	rest := strings.TrimPrefix(name, "generation-jobs-")
	parts := strings.SplitN(rest, "-", 2)
	if len(parts) != 2 {
		return rest
	}
	return parts[0] + "/" + parts[1]
}

// PurgeQueue empties a queue. Audited.
func (s *Service) PurgeQueue(ctx context.Context, name string) error {
	url, ok := s.queueURL(name)
	if !ok {
		return fmt.Errorf("ops: queue %q not known", name)
	}
	if _, err := s.SQS.PurgeQueue(ctx, &sqs.PurgeQueueInput{QueueUrl: aws.String(url)}); err != nil {
		return fmt.Errorf("ops: purge queue: %w", err)
	}
	s.audit(ctx, AuditEvent{Operation: "PurgeQueue", Target: name})
	return nil
}

// RedriveDlq pumps up to limit messages from the named DLQ back to its
// source queue. Returns counts of moved + failed messages.
func (s *Service) RedriveDlq(ctx context.Context, name string, limit int32) (int32, int32, error) {
	info, ok := s.DLQs[name]
	if !ok {
		return 0, 0, fmt.Errorf("ops: dlq %q not known", name)
	}
	if info.SourceURL == "" {
		return 0, 0, fmt.Errorf("ops: dlq %q has no source queue", name)
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	var moved, failed int32
	for moved+failed < limit {
		batch := int32(10)
		if remaining := limit - (moved + failed); remaining < batch {
			batch = remaining
		}
		out, err := s.SQS.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:            aws.String(info.URL),
			MaxNumberOfMessages: batch,
			WaitTimeSeconds:     0,
			VisibilityTimeout:   30,
		})
		if err != nil {
			return moved, failed, err
		}
		if len(out.Messages) == 0 {
			break
		}
		for _, msg := range out.Messages {
			_, err := s.SQS.SendMessage(ctx, &sqs.SendMessageInput{
				QueueUrl:    aws.String(info.SourceURL),
				MessageBody: msg.Body,
			})
			if err != nil {
				failed++
				continue
			}
			if _, err := s.SQS.DeleteMessage(ctx, &sqs.DeleteMessageInput{
				QueueUrl:      aws.String(info.URL),
				ReceiptHandle: msg.ReceiptHandle,
			}); err != nil {
				failed++
				continue
			}
			moved++
		}
	}
	s.audit(ctx, AuditEvent{
		Operation: "RedriveDlq",
		Target:    name,
		Details:   map[string]string{"moved": fmt.Sprintf("%d", moved), "failed": fmt.Sprintf("%d", failed)},
	})
	return moved, failed, nil
}

func (s *Service) queueURL(name string) (string, bool) {
	if u, ok := s.QueueURLs[name]; ok {
		return u, true
	}
	if info, ok := s.DLQs[name]; ok {
		return info.URL, true
	}
	return "", false
}
