// Package admin provides DLQ inspect/replay operations for operators.
//
// Replay is signed: Peek returns body_signature (HMAC over body+attrs) and
// Replay verifies it before re-sending so a malicious operator cannot inject
// a different body than what was in the DLQ.
package admin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus"
)

var (
	ErrDLQReplayInvalidSignature = errors.New("dlq: invalid signature")
	ErrDLQReplaySend             = errors.New("dlq: replay send failed")
	ErrDLQReplayDeleteAfterSend  = errors.New("dlq: replay delete-after-send failed")
	ErrDLQReplayInFlight         = errors.New("dlq: replay in flight")
	ErrDLQReplayConflict         = errors.New("dlq: replay conflict")
	// ErrDLQReplayPermanentlyFailed is returned when the prior replay attempt
	// persisted a terminal failure on the same (dlq, message id, body) tuple.
	// The idempotency store has no reset operation — re-calling Claim would
	// keep returning the same outcome. Operators must construct a new message
	// (different id) or add a deliberate reset feature to recover.
	ErrDLQReplayPermanentlyFailed = errors.New("dlq: prior replay permanently failed")
)

const (
	replayLeaseTTL        = 2 * time.Minute
	peekVisibilityTimeout = 60 * time.Second
)

// ReplayPhaseError identifies which replay side-effect failed.
type ReplayPhaseError struct {
	Phase string
	Err   error
}

func (e *ReplayPhaseError) Error() string {
	return "dlq.Replay: " + e.Phase + ": " + e.Err.Error()
}

func (e *ReplayPhaseError) Unwrap() error { return e.Err }

// DLQTransport is the subset of eventbus.Consumer that admin needs. The full
// Consumer interface plus the DLQ-specific Peek/Delete helpers satisfy this.
type DLQTransport interface {
	PeekDLQ(ctx context.Context, queueURL string, max int32, visibility time.Duration) ([]eventbus.Message, error)
	DeleteFromQueue(ctx context.Context, queueURL, receiptHandle string) error
	Send(ctx context.Context, queueURL string, body []byte, attrs map[string]string) (string, error)
	Purge(ctx context.Context, queueURL string) error
	Attributes(ctx context.Context, queueURL string) (map[string]string, error)
}

// DLQInfo describes one DLQ and its source.
type DLQInfo struct {
	Name      string
	URL       string
	SourceURL string
}

// DLQAdmin lets operators inspect dead-letter queues and replay their
// messages. Idempotency-bracketed replays are safe to retry.
type DLQAdmin struct {
	T        DLQTransport
	Secret   []byte
	DLQs     map[string]DLQInfo
	idem     idempotency.Store
	recorder auditapp.Recorder
}

// NewDLQAdmin builds the admin atop a DLQTransport, idempotency store, and
// the audit Recorder used by successful Replay calls. Pass nil for the
// recorder to opt out (it'll default to NoopRecorder).
func NewDLQAdmin(t DLQTransport, idem idempotency.Store, recorder auditapp.Recorder) *DLQAdmin {
	if recorder == nil {
		recorder = auditapp.NoopRecorder{}
	}
	return &DLQAdmin{T: t, DLQs: map[string]DLQInfo{}, idem: idem, recorder: recorder}
}

// SetTopology replaces the DLQ topology and the signing secret.
func (a *DLQAdmin) SetTopology(secret []byte, dlqs map[string]DLQInfo) {
	a.Secret = secret
	a.DLQs = dlqs
}

// Status returns the approximate depth for every configured DLQ.
func (a *DLQAdmin) Status(ctx context.Context) ([]DLQQueueStatus, error) {
	names := make([]string, 0, len(a.DLQs))
	for name := range a.DLQs {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]DLQQueueStatus, 0, len(names))
	for _, name := range names {
		info := a.DLQs[name]
		attrs, err := a.T.Attributes(ctx, info.URL)
		if err != nil {
			return nil, err
		}
		count := int32(0)
		if v, ok := attrs["ApproximateNumberOfMessages"]; ok {
			n, _ := strconv.Atoi(v)
			count = int32(n)
		}
		out = append(out, DLQQueueStatus{Name: name, URL: info.URL, SourceURL: info.SourceURL, Count: count})
	}
	return out, nil
}

// DLQQueueStatus is one queue's depth snapshot.
type DLQQueueStatus struct {
	Name      string
	URL       string
	SourceURL string
	Count     int32
}

// Peek returns up to `max` messages from the named DLQ, each carrying a
// body_signature so Replay can verify the body hasn't been swapped.
func (a *DLQAdmin) Peek(ctx context.Context, dlqName string, max int32) ([]DLQMessage, error) {
	info, ok := a.DLQs[dlqName]
	if !ok {
		return nil, fmt.Errorf("dlq.Peek: unknown dlq %q", dlqName)
	}
	if max <= 0 {
		max = 10
	}
	if max > 10 {
		max = 10
	}
	msgs, err := a.T.PeekDLQ(ctx, info.URL, max, peekVisibilityTimeout)
	if err != nil {
		return nil, fmt.Errorf("dlq.Peek: %w", err)
	}
	rows := make([]DLQMessage, 0, len(msgs))
	for _, m := range msgs {
		row := DLQMessage{
			ID:                m.ID,
			ReceiptHandle:     m.ReceiptHandle,
			Body:              string(m.Body),
			MessageAttributes: m.Attributes,
		}
		row.BodySignature = a.signBody(dlqName, row.ID, row.Body, row.MessageAttributes)
		rows = append(rows, row)
	}
	return rows, nil
}

// Replay is the actor-less entry point retained for callers that haven't
// resolved the operator's identity. Transports that already have it should
// invoke ReplayAs so the audit row threads on a non-empty ACTOR# GSI.
func (a *DLQAdmin) Replay(ctx context.Context, dlqName string, msg DLQMessageInput) (string, error) {
	return a.ReplayAs(ctx, dlqName, "", "", msg)
}

// ReplayAs verifies + re-sends one message to its source queue, then deletes
// the DLQ copy. Returns the new source MessageID on success. tenantID and
// actorUserID are stamped onto the audit row so dashboards can group
// replays by operator and tenant.
func (a *DLQAdmin) ReplayAs(ctx context.Context, dlqName, tenantID, actorUserID string, msg DLQMessageInput) (string, error) {
	info, ok := a.DLQs[dlqName]
	if !ok {
		return "", fmt.Errorf("dlq.Replay: unknown dlq %q", dlqName)
	}
	if msg.ReceiptHandle == "" {
		return "", errors.New("dlq.Replay: receipt_handle required")
	}
	wantSig := a.signBody(dlqName, msg.ID, msg.Body, msg.MessageAttributes)
	if !hmac.Equal([]byte(wantSig), []byte(msg.BodySignature)) {
		return "", ErrDLQReplayInvalidSignature
	}

	var claimToken string
	scope := "REPLAY#" + dlqName + "#" + msg.ID
	if a.idem != nil {
		acquired, err := idempotency.Acquire(ctx, a.idem, scope, msg.BodySignature, replayLeaseTTL)
		if err != nil {
			return "", fmt.Errorf("dlq.Replay: %w", err)
		}
		switch acquired.Kind {
		case idempotency.AcquireOwned, idempotency.AcquireReclaimed:
			claimToken = acquired.Token
		case idempotency.AcquireCompleted:
			// A prior attempt may have sent successfully, completed the
			// idempotency row, then failed to delete the DLQ copy. Retry the
			// source delete when the caller still has a receipt handle so the
			// replay can converge without sending a duplicate message.
			if msg.ReceiptHandle != "" {
				if derr := a.T.DeleteFromQueue(ctx, info.URL, msg.ReceiptHandle); derr != nil {
					return "", &ReplayPhaseError{Phase: "delete-after-completed", Err: errors.Join(ErrDLQReplayDeleteAfterSend, derr)}
				}
			}
			return acquired.CachedRef, nil
		case idempotency.AcquireInFlight:
			return "", fmt.Errorf("dlq.Replay: %w", ErrDLQReplayInFlight)
		case idempotency.AcquirePermanentlyFailed:
			// The persisted row is terminal. Re-Claiming returns the same
			// outcome forever — operators need a deliberate reset to recover,
			// which is intentionally not exposed here.
			return "", fmt.Errorf("dlq.Replay: %w", ErrDLQReplayPermanentlyFailed)
		case idempotency.AcquireInputConflict:
			return "", fmt.Errorf("dlq.Replay: %w", ErrDLQReplayConflict)
		}
	}

	newMsgID, err := a.T.Send(ctx, info.SourceURL, []byte(msg.Body), msg.MessageAttributes)
	if err != nil {
		if a.idem != nil && claimToken != "" {
			_ = a.idem.Fail(ctx, scope, claimToken, "SEND_FAILED")
		}
		return "", &ReplayPhaseError{Phase: "send", Err: errors.Join(ErrDLQReplaySend, err)}
	}

	if a.idem != nil && claimToken != "" {
		_ = a.idem.Complete(ctx, scope, claimToken, newMsgID)
	}

	if derr := a.T.DeleteFromQueue(ctx, info.URL, msg.ReceiptHandle); derr != nil {
		return "", &ReplayPhaseError{Phase: "delete-after-send", Err: errors.Join(ErrDLQReplayDeleteAfterSend, derr)}
	}

	// Audit only after the send+delete pair has succeeded. An audit row on
	// a partial replay would mislead operators investigating retries.
	a.recordReplay(ctx, dlqName, tenantID, actorUserID, msg.ID, newMsgID)
	return newMsgID, nil
}

// recordReplay routes the replay event to the matching audit family.
// DLQ names starting with "outbox-" or containing "-outbox-" map to
// outbox.dlq.replayed; everything else falls under sqs.dlq.replayed.
// Dashboards filter on the family prefix so the split matters even when
// the row body is identical.
func (a *DLQAdmin) recordReplay(ctx context.Context, dlqName, tenantID, actorUserID, originalID, newID string) {
	var ev audit.Event
	if strings.HasPrefix(dlqName, "outbox-") || strings.Contains(dlqName, "-outbox-") {
		ev = auditapp.NewOutboxDLQReplayed(tenantID, actorUserID, dlqName, originalID, newID, "")
	} else {
		ev = auditapp.NewSQSDLQReplayed(tenantID, actorUserID, dlqName, originalID, newID, "")
	}
	_ = a.recorder.Record(ctx, ev)
}

// Delete removes a single message from a DLQ.
func (a *DLQAdmin) Delete(ctx context.Context, dlqName, receiptHandle string) error {
	info, ok := a.DLQs[dlqName]
	if !ok {
		return fmt.Errorf("dlq.Delete: unknown dlq %q", dlqName)
	}
	if receiptHandle == "" {
		return errors.New("dlq.Delete: receipt_handle required")
	}
	if err := a.T.DeleteFromQueue(ctx, info.URL, receiptHandle); err != nil {
		return fmt.Errorf("dlq.Delete: %w", err)
	}
	return nil
}

// Purge empties a DLQ. SQS allows one purge per queue per minute.
func (a *DLQAdmin) Purge(ctx context.Context, dlqName string) error {
	info, ok := a.DLQs[dlqName]
	if !ok {
		return fmt.Errorf("dlq.Purge: unknown dlq %q", dlqName)
	}
	if err := a.T.Purge(ctx, info.URL); err != nil {
		return fmt.Errorf("dlq.Purge: %w", err)
	}
	return nil
}

// signBody returns the HMAC-SHA256 hex of (dlq|id|body|canonical(attrs)).
func (a *DLQAdmin) signBody(dlq, id, body string, attrs map[string]string) string {
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	canon := strings.Builder{}
	for _, k := range keys {
		canon.WriteString(k)
		canon.WriteString("=")
		canon.WriteString(attrs[k])
		canon.WriteString("&")
	}
	mac := hmac.New(sha256.New, a.Secret)
	mac.Write([]byte(dlq + "|" + id + "|" + body + "|" + canon.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

// DLQMessage is the inspected shape returned by Peek.
type DLQMessage struct {
	ID                string
	Body              string
	ReceiptHandle     string
	Attributes        map[string]string
	MessageAttributes map[string]string
	BodySignature     string
}

// DLQMessageInput is the shape Replay expects (echoed from Peek).
type DLQMessageInput struct {
	ID                string
	ReceiptHandle     string
	Body              string
	MessageAttributes map[string]string
	BodySignature     string
}
