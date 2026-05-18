// Package webhook implements outbound webhook signing and dispatch.
//
// Signing contract:
//
//	X-Webhook-Timestamp: <epoch seconds, base 10>
//	X-Webhook-Signature: sha256=<hex(HMAC_SHA256(secret, timestamp + "." + body))>
//
// Receivers MUST reject if timestamp drift exceeds the allowed window
// (default 5 minutes).
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const (
	HeaderTimestamp = "X-Webhook-Timestamp"
	HeaderSignature = "X-Webhook-Signature"
	HeaderEventID   = "X-Webhook-Event-Id"
	HeaderEventType = "X-Webhook-Event-Type"

	// MaxClockSkew is the receiver-side timestamp drift window.
	MaxClockSkew = 5 * time.Minute
)

// Sign returns the canonical signature header value for the given timestamp,
// body, and secret. The header is the form "sha256=<hex>".
func Sign(secret []byte, timestamp time.Time, body []byte) string {
	mac := hmac.New(sha256.New, secret)
	ts := strconv.FormatInt(timestamp.Unix(), 10)
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// SetHeaders applies the timestamp + signature headers to req.
func SetHeaders(req *http.Request, secret []byte, body []byte, timestamp time.Time) {
	req.Header.Set(HeaderTimestamp, strconv.FormatInt(timestamp.Unix(), 10))
	req.Header.Set(HeaderSignature, Sign(secret, timestamp, body))
}

// Verify recomputes the signature from the secret + timestamp + body and
// returns nil iff the provided signature matches and the timestamp is within
// MaxClockSkew. Use constant-time comparison to defeat timing attacks.
func Verify(secret []byte, timestampHeader, signatureHeader string, body []byte) error {
	if timestampHeader == "" || signatureHeader == "" {
		return errors.New("webhook: missing signature/timestamp header")
	}
	ts, err := strconv.ParseInt(timestampHeader, 10, 64)
	if err != nil {
		return fmt.Errorf("webhook: invalid timestamp: %w", err)
	}
	t := time.Unix(ts, 0)
	if drift := time.Since(t); drift < -MaxClockSkew || drift > MaxClockSkew {
		return fmt.Errorf("webhook: timestamp drift %s exceeds %s", drift, MaxClockSkew)
	}
	expected := Sign(secret, t, body)
	if !hmac.Equal([]byte(expected), []byte(signatureHeader)) {
		return errors.New("webhook: signature mismatch")
	}
	return nil
}
