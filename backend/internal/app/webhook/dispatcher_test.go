package webhook

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/events"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
)

// fakeStore is an idempotency.Store with controllable Complete/Claim returns.
type fakeStore struct {
	outcome     idempotency.Outcome
	token       string
	getRef      string
	completeErr error
}

func (s *fakeStore) Claim(_ context.Context, _, _ string, _ time.Duration) (idempotency.Outcome, string, error) {
	return s.outcome, s.token, nil
}
func (s *fakeStore) Complete(_ context.Context, _, _, _ string) error { return s.completeErr }
func (s *fakeStore) Fail(_ context.Context, _, _, _ string) error     { return nil }
func (s *fakeStore) GetResult(_ context.Context, _ string) (string, idempotency.Status, error) {
	return s.getRef, idempotency.StatusCompleted, nil
}
func (s *fakeStore) Reclaim(_ context.Context, _ string, _ time.Duration) (string, error) {
	return s.token, nil
}
func (s *fakeStore) Abandon(_ context.Context, _, _ string) error { return nil }

func envFor(url string) events.WebhookDeliveryEnvelope {
	return events.WebhookDeliveryEnvelope{
		WebhookURL: url,
		EventID:    "evt-1",
		DeliveryID: "del-1",
		EventType:  "media.uploaded",
		TenantID:   "tnt",
		MediaID:    "med",
		Payload:    []byte(`{"x":1}`),
	}
}

type staticResolver map[string][]net.IPAddr

func (r staticResolver) LookupIPAddr(_ context.Context, host string) ([]net.IPAddr, error) {
	addrs, ok := r[host]
	if !ok {
		return nil, errors.New("host not found")
	}
	return addrs, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func okClient(called *bool) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		*called = true
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("ok")),
			Request:    req,
		}, nil
	})}
}

// TestSend_CompleteFailsAfter2xx_SwallowsError locks the policy: once the
// customer endpoint has accepted the POST, a failure to flip the idempotency
// row to COMPLETED must NOT propagate, because returning an error makes SQS
// redeliver the message — which means a duplicate POST. A stale CLAIMED row
// is acceptable; a duplicate side-effect is not.
func TestSend_CompleteFailsAfter2xx_SwallowsError(t *testing.T) {
	store := &fakeStore{
		outcome:     idempotency.OutcomeNew,
		token:       "tok",
		completeErr: errors.New("ddb timed out"),
	}
	var called bool
	d := NewDispatcher([]byte("secret"))
	d.Client = okClient(&called)
	d.Resolver = staticResolver{"webhook.test": {{IP: net.ParseIP("93.184.216.34")}}}
	d.Idempotency = store

	if err := d.Send(context.Background(), envFor("https://webhook.test/hook")); err != nil {
		t.Fatalf("Send returned %v; post-2xx Complete failure must be swallowed", err)
	}
	if !called {
		t.Fatalf("HTTP endpoint was not hit")
	}
}

// TestSend_AcquireCompleted_SkipsPOST exercises the cached-result path: a
// REPLAY_COMPLETED outcome short-circuits the customer call.
func TestSend_AcquireCompleted_SkipsPOST(t *testing.T) {
	var called bool
	store := &fakeStore{outcome: idempotency.OutcomeReplayCompleted}
	d := NewDispatcher([]byte("secret"))
	d.Client = okClient(&called)
	d.Idempotency = store

	if err := d.Send(context.Background(), envFor("https://webhook.test/hook")); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if called {
		t.Fatalf("HTTP endpoint hit despite REPLAY_COMPLETED; duplicate delivery")
	}
}

func TestSendRejectsPrivateDNSResultBeforePOST(t *testing.T) {
	var called bool
	d := NewDispatcher([]byte("secret"))
	d.Client = okClient(&called)
	d.Resolver = staticResolver{"webhook.test": {{IP: net.ParseIP("10.0.0.7")}}}
	d.MaxRetries = 0

	if err := d.Send(context.Background(), envFor("https://webhook.test/hook")); err == nil {
		t.Fatalf("Send accepted private DNS target")
	}
	if called {
		t.Fatalf("HTTP endpoint hit for private DNS target")
	}
}

func TestSendRejectsMetadataIPLiteralBeforePOST(t *testing.T) {
	var called bool
	d := NewDispatcher([]byte("secret"))
	d.Client = okClient(&called)
	d.MaxRetries = 0

	if err := d.Send(context.Background(), envFor("https://169.254.169.254/latest/meta-data")); err == nil {
		t.Fatalf("Send accepted metadata IP literal")
	}
	if called {
		t.Fatalf("HTTP endpoint hit for metadata IP literal")
	}
}

func TestSendRejectsSpecialUseDNSResultBeforePOST(t *testing.T) {
	var called bool
	d := NewDispatcher([]byte("secret"))
	d.Client = okClient(&called)
	d.Resolver = staticResolver{"webhook.test": {{IP: net.ParseIP("203.0.113.10")}}}
	d.MaxRetries = 0

	if err := d.Send(context.Background(), envFor("https://webhook.test/hook")); err == nil {
		t.Fatalf("Send accepted special-use DNS target")
	}
	if called {
		t.Fatalf("HTTP endpoint hit for special-use DNS target")
	}
}

func TestSendRejectsRedirectToPrivateTarget(t *testing.T) {
	var called int
	d := NewDispatcher([]byte("secret"))
	d.Resolver = staticResolver{
		"webhook.test":  {{IP: net.ParseIP("93.184.216.34")}},
		"internal.test": {{IP: net.ParseIP("127.0.0.1")}},
	}
	d.Client = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		called++
		resp := &http.Response{
			StatusCode: http.StatusFound,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("redirect")),
			Request:    req,
		}
		resp.Header.Set("Location", "https://internal.test/hook")
		return resp, nil
	})}
	d.MaxRetries = 0

	if err := d.Send(context.Background(), envFor("https://webhook.test/hook")); err == nil {
		t.Fatalf("Send accepted redirect to private target")
	}
	if called != 1 {
		t.Fatalf("transport calls = %d, want initial request only", called)
	}
}
