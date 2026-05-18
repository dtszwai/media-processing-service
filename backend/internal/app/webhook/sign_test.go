package webhook_test

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/webhook"
)

func TestSignAndVerify_Roundtrip(t *testing.T) {
	secret := []byte("test-secret")
	body := []byte(`{"event":"media.completed","media_id":"m1"}`)
	ts := time.Now()

	sig := webhook.Sign(secret, ts, body)
	if !strings.HasPrefix(sig, "sha256=") {
		t.Fatalf("signature missing sha256= prefix: %q", sig)
	}
	if err := webhook.Verify(secret, strconv.FormatInt(ts.Unix(), 10), sig, body); err != nil {
		t.Fatalf("verify: %v", err)
	}
}

func TestVerify_RejectsTimestampDrift(t *testing.T) {
	secret := []byte("test-secret")
	body := []byte("payload")
	ts := time.Now().Add(-10 * time.Minute)
	sig := webhook.Sign(secret, ts, body)
	if err := webhook.Verify(secret, strconv.FormatInt(ts.Unix(), 10), sig, body); err == nil {
		t.Fatalf("expected drift rejection, got nil")
	}
}

func TestVerify_RejectsMutatedBody(t *testing.T) {
	secret := []byte("test-secret")
	ts := time.Now()
	sig := webhook.Sign(secret, ts, []byte("original"))
	if err := webhook.Verify(secret, strconv.FormatInt(ts.Unix(), 10), sig, []byte("tampered")); err == nil {
		t.Fatalf("expected mismatch, got nil")
	}
}

func TestSetHeaders(t *testing.T) {
	req := httptest.NewRequest("POST", "https://example/hooks", strings.NewReader("body"))
	webhook.SetHeaders(req, []byte("sec"), []byte("body"), time.Now())
	if req.Header.Get(webhook.HeaderTimestamp) == "" {
		t.Fatalf("missing timestamp header")
	}
	if !strings.HasPrefix(req.Header.Get(webhook.HeaderSignature), "sha256=") {
		t.Fatalf("bad signature: %q", req.Header.Get(webhook.HeaderSignature))
	}
}
