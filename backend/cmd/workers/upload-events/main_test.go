package main

import (
	"testing"
)

// TestIsS3TestEvent locks the recognition of S3's notification-config probe
// envelope. The worker silently drops the probe so the queue doesn't DLQ on
// the one-time wake-up message every newly-configured topic ships.
func TestIsS3TestEvent(t *testing.T) {
	t.Run("probe envelope", func(t *testing.T) {
		body := []byte(`{"Service":"Amazon S3","Event":"s3:TestEvent","Bucket":"b","Time":"2026-01-01T00:00:00Z","RequestId":"r","HostId":"h"}`)
		if !isS3TestEvent(body) {
			t.Fatal("expected isS3TestEvent=true on probe envelope")
		}
	})
	t.Run("real notification", func(t *testing.T) {
		body := []byte(`{"Records":[{"eventName":"ObjectCreated:Put"}]}`)
		if isS3TestEvent(body) {
			t.Fatal("expected isS3TestEvent=false on real notification")
		}
	})
}
