package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
)

// mockIdem is a minimal idempotency.Store for unit tests.
type mockIdem struct {
	claimOutcome idempotency.Outcome
	claimToken   string
	claimErr     error
	getResultRef string
	getResultErr error
	reclaimToken string
	reclaimErr   error
	completeErr  error
	failErr      error

	claimCalls     int
	completedScope string
	completedRef   string
	failedCode     string
}

func (m *mockIdem) Claim(_ context.Context, _, _ string, _ time.Duration) (idempotency.Outcome, string, error) {
	m.claimCalls++
	return m.claimOutcome, m.claimToken, m.claimErr
}

func (m *mockIdem) Complete(_ context.Context, scope, _, ref string) error {
	m.completedScope = scope
	m.completedRef = ref
	return m.completeErr
}

func (m *mockIdem) Fail(_ context.Context, _, _, code string) error {
	m.failedCode = code
	return m.failErr
}

func (m *mockIdem) GetResult(_ context.Context, _ string) (string, idempotency.Status, error) {
	return m.getResultRef, idempotency.StatusCompleted, m.getResultErr
}

func (m *mockIdem) Reclaim(_ context.Context, _ string, _ time.Duration) (string, error) {
	return m.reclaimToken, m.reclaimErr
}

func (m *mockIdem) Abandon(_ context.Context, _, _ string) error { return nil }

func setupAdminWithIdem(idem idempotency.Store, transport DLQTransport) *DLQAdmin {
	a := NewDLQAdmin(transport, idem, nil)
	a.SetTopology([]byte("secret"), map[string]DLQInfo{
		"dlq": {Name: "dlq", URL: "https://sqs.test/dlq", SourceURL: "https://sqs.test/src"},
	})
	return a
}

func validInput(a *DLQAdmin) DLQMessageInput {
	sig := a.signBody("dlq", "msg-1", "body", nil)
	return DLQMessageInput{
		ID:            "msg-1",
		ReceiptHandle: "rh",
		Body:          "body",
		BodySignature: sig,
	}
}

func sendOnly(msgID string) *fakeDLQTransport {
	return &fakeDLQTransport{
		send: func(_ context.Context, _ string, _ []byte, _ map[string]string) (string, error) {
			return msgID, nil
		},
	}
}

func sendFails() *fakeDLQTransport {
	return &fakeDLQTransport{
		send: func(_ context.Context, _ string, _ []byte, _ map[string]string) (string, error) {
			return "", errors.New("oops")
		},
	}
}

func TestReplay_New_SuccessfulSendAndDelete(t *testing.T) {
	idem := &mockIdem{claimOutcome: idempotency.OutcomeNew, claimToken: "tok"}
	a := setupAdminWithIdem(idem, sendOnly("new-id"))
	newID, err := a.Replay(context.Background(), "dlq", validInput(a))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newID != "new-id" {
		t.Fatalf("newID = %q, want new-id", newID)
	}
	if idem.completedRef != "new-id" {
		t.Fatalf("Complete ref = %q, want new-id", idem.completedRef)
	}
}

func TestReplay_ReplayCompleted_ReturnsCachedID(t *testing.T) {
	idem := &mockIdem{claimOutcome: idempotency.OutcomeReplayCompleted, getResultRef: "cached-id"}
	a := setupAdminWithIdem(idem, &fakeDLQTransport{})
	newID, err := a.Replay(context.Background(), "dlq", validInput(a))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newID != "cached-id" {
		t.Fatalf("newID = %q, want cached-id", newID)
	}
}

func TestReplay_ReplayCompletedDeletesDLQCopy(t *testing.T) {
	idem := &mockIdem{claimOutcome: idempotency.OutcomeReplayCompleted, getResultRef: "cached-id"}
	var deleteQueue, deleteReceipt string
	var sendCalls int
	transport := &fakeDLQTransport{
		send: func(_ context.Context, _ string, _ []byte, _ map[string]string) (string, error) {
			sendCalls++
			return "should-not-send", nil
		},
		deleteFrom: func(_ context.Context, queueURL, receipt string) error {
			deleteQueue = queueURL
			deleteReceipt = receipt
			return nil
		},
	}
	a := setupAdminWithIdem(idem, transport)
	newID, err := a.Replay(context.Background(), "dlq", validInput(a))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newID != "cached-id" {
		t.Fatalf("newID = %q, want cached-id", newID)
	}
	if sendCalls != 0 {
		t.Fatalf("send calls = %d, want 0", sendCalls)
	}
	if deleteQueue != "https://sqs.test/dlq" || deleteReceipt != "rh" {
		t.Fatalf("delete = (%q, %q), want dlq URL and receipt", deleteQueue, deleteReceipt)
	}
}

func TestReplay_ReplayClaimedFresh_ReturnsInFlight(t *testing.T) {
	idem := &mockIdem{claimOutcome: idempotency.OutcomeReplayClaimedFresh}
	a := setupAdminWithIdem(idem, &fakeDLQTransport{})
	_, err := a.Replay(context.Background(), "dlq", validInput(a))
	if !errors.Is(err, ErrDLQReplayInFlight) {
		t.Fatalf("err = %v, want ErrDLQReplayInFlight", err)
	}
}

func TestReplay_ReplayClaimedStale_ReclaimsAndProceeds(t *testing.T) {
	idem := &mockIdem{claimOutcome: idempotency.OutcomeReplayClaimedStale, reclaimToken: "new-tok"}
	a := setupAdminWithIdem(idem, sendOnly("reclaimed-id"))
	newID, err := a.Replay(context.Background(), "dlq", validInput(a))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if newID != "reclaimed-id" {
		t.Fatalf("newID = %q, want reclaimed-id", newID)
	}
}

func TestReplay_ReplayClaimedStale_ReclaimFails_ReturnsError(t *testing.T) {
	idem := &mockIdem{
		claimOutcome: idempotency.OutcomeReplayClaimedStale,
		reclaimErr:   errors.New("condition failed"),
	}
	a := setupAdminWithIdem(idem, &fakeDLQTransport{})
	_, err := a.Replay(context.Background(), "dlq", validInput(a))
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
}

// TestReplay_ReplayFailed_ReturnsPermanentlyFailed locks the new policy:
// a previously-failed replay row is terminal. The DDB store has no reset
// operation, so re-Claiming would loop on OutcomeReplayFailed forever. We
// surface a typed error and require operators to provide a fresh message id
// or build a deliberate reset feature.
func TestReplay_ReplayFailed_ReturnsPermanentlyFailed(t *testing.T) {
	idem := &mockIdem{claimOutcome: idempotency.OutcomeReplayFailed}
	a := setupAdminWithIdem(idem, &fakeDLQTransport{})
	_, err := a.Replay(context.Background(), "dlq", validInput(a))
	if !errors.Is(err, ErrDLQReplayPermanentlyFailed) {
		t.Fatalf("err = %v, want ErrDLQReplayPermanentlyFailed", err)
	}
	if idem.claimCalls != 1 {
		t.Fatalf("Claim called %d times, want 1 (no re-Claim loop)", idem.claimCalls)
	}
}

func TestReplay_Conflict_ReturnsConflict(t *testing.T) {
	idem := &mockIdem{claimOutcome: idempotency.OutcomeConflict}
	a := setupAdminWithIdem(idem, &fakeDLQTransport{})
	_, err := a.Replay(context.Background(), "dlq", validInput(a))
	if !errors.Is(err, ErrDLQReplayConflict) {
		t.Fatalf("err = %v, want ErrDLQReplayConflict", err)
	}
}

func TestReplay_SendFail_CallsIdemFail(t *testing.T) {
	idem := &mockIdem{claimOutcome: idempotency.OutcomeNew, claimToken: "tok"}
	a := setupAdminWithIdem(idem, sendFails())
	_, err := a.Replay(context.Background(), "dlq", validInput(a))
	if !errors.Is(err, ErrDLQReplaySend) {
		t.Fatalf("err = %v, want ErrDLQReplaySend", err)
	}
	if idem.failedCode != "SEND_FAILED" {
		t.Fatalf("failedCode = %q, want SEND_FAILED", idem.failedCode)
	}
}
