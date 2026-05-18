package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus"
)

// fakeDLQTransport implements DLQTransport using in-memory hooks so admin
// tests run without an SDK client.
type fakeDLQTransport struct {
	peek       func(ctx context.Context, queueURL string, max int32, visibility time.Duration) ([]eventbus.Message, error)
	deleteFrom func(ctx context.Context, queueURL, receipt string) error
	send       func(ctx context.Context, queueURL string, body []byte, attrs map[string]string) (string, error)
	purge      func(ctx context.Context, queueURL string) error
	attrs      func(ctx context.Context, queueURL string) (map[string]string, error)
}

func (f *fakeDLQTransport) PeekDLQ(ctx context.Context, q string, m int32, v time.Duration) ([]eventbus.Message, error) {
	if f.peek == nil {
		return nil, nil
	}
	return f.peek(ctx, q, m, v)
}

func (f *fakeDLQTransport) DeleteFromQueue(ctx context.Context, q, h string) error {
	if f.deleteFrom == nil {
		return nil
	}
	return f.deleteFrom(ctx, q, h)
}

func (f *fakeDLQTransport) Send(ctx context.Context, q string, body []byte, attrs map[string]string) (string, error) {
	if f.send == nil {
		return "", nil
	}
	return f.send(ctx, q, body, attrs)
}

func (f *fakeDLQTransport) Purge(ctx context.Context, q string) error {
	if f.purge == nil {
		return nil
	}
	return f.purge(ctx, q)
}

func (f *fakeDLQTransport) Attributes(ctx context.Context, q string) (map[string]string, error) {
	if f.attrs == nil {
		return map[string]string{}, nil
	}
	return f.attrs(ctx, q)
}

func TestDLQReplayRejectsTamperedMessageAttributes(t *testing.T) {
	a := NewDLQAdmin(&fakeDLQTransport{}, nil, nil)
	a.SetTopology([]byte("test-secret"), map[string]DLQInfo{
		"dlq": {Name: "dlq", URL: "https://sqs.test/dlq", SourceURL: "https://sqs.test/src"},
	})
	sig := a.signBody("dlq", "msg-1", "body", map[string]string{"tenant": "a"})

	_, err := a.Replay(context.Background(), "dlq", DLQMessageInput{
		ID:                "msg-1",
		ReceiptHandle:     "rh",
		Body:              "body",
		MessageAttributes: map[string]string{"tenant": "b"},
		BodySignature:     sig,
	})
	if !errors.Is(err, ErrDLQReplayInvalidSignature) {
		t.Fatalf("Replay error = %v, want INVALID_SIGNATURE", err)
	}
}

func TestDLQReplayClassifiesSendFailure(t *testing.T) {
	transport := &fakeDLQTransport{
		send: func(_ context.Context, _ string, _ []byte, _ map[string]string) (string, error) {
			return "", errors.New("send failed")
		},
	}
	a := NewDLQAdmin(transport, nil, nil)
	a.SetTopology([]byte("test-secret"), map[string]DLQInfo{
		"dlq": {Name: "dlq", URL: "https://sqs.test/dlq", SourceURL: "https://sqs.test/src"},
	})
	sig := a.signBody("dlq", "msg-1", "body", nil)

	_, err := a.Replay(context.Background(), "dlq", DLQMessageInput{
		ID:            "msg-1",
		ReceiptHandle: "rh",
		Body:          "body",
		BodySignature: sig,
	})
	if !errors.Is(err, ErrDLQReplaySend) {
		t.Fatalf("Replay error = %v, want send classification", err)
	}
	var phase *ReplayPhaseError
	if !errors.As(err, &phase) || phase.Phase != "send" {
		t.Fatalf("Replay phase = %#v, want send", phase)
	}
}

func TestDLQReplayClassifiesDeleteAfterSendFailure(t *testing.T) {
	var sendCalls int
	transport := &fakeDLQTransport{
		send: func(_ context.Context, _ string, _ []byte, _ map[string]string) (string, error) {
			sendCalls++
			return "new-msg", nil
		},
		deleteFrom: func(_ context.Context, _, _ string) error {
			return errors.New("delete failed")
		},
	}
	a := NewDLQAdmin(transport, nil, nil)
	a.SetTopology([]byte("test-secret"), map[string]DLQInfo{
		"dlq": {Name: "dlq", URL: "https://sqs.test/dlq", SourceURL: "https://sqs.test/src"},
	})
	sig := a.signBody("dlq", "msg-1", "body", nil)

	_, err := a.Replay(context.Background(), "dlq", DLQMessageInput{
		ID:            "msg-1",
		ReceiptHandle: "rh",
		Body:          "body",
		BodySignature: sig,
	})
	if sendCalls != 1 {
		t.Fatalf("send calls = %d, want 1", sendCalls)
	}
	if !errors.Is(err, ErrDLQReplayDeleteAfterSend) {
		t.Fatalf("Replay error = %v, want delete-after-send classification", err)
	}
	var phase *ReplayPhaseError
	if !errors.As(err, &phase) || phase.Phase != "delete-after-send" {
		t.Fatalf("Replay phase = %#v, want delete-after-send", phase)
	}
}
