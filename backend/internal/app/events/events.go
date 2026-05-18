// Package events defines the JSON envelope shape passed through SNS/SQS for
// media and webhook events.
package events

import "time"

// EventType is the canonical event_type string used as an SNS message
// attribute and in the body.
type EventType string

const (
	EventMediaProcess   EventType = "media.v1.process"
	EventMediaCompleted EventType = "media.v1.completed"
	EventMediaFailed    EventType = "media.v1.failed"
	EventMediaDelete    EventType = "media.v1.delete"
	EventMediaCleanup   EventType = "media.v1.cleanup"
)

// MediaEvent is the message published to media-management-topic for downstream
// derivation/cleanup. Includes traceparent for OTel propagation.
type MediaEvent struct {
	MessageID   string    `json:"message_id"`
	EventType   EventType `json:"event_type"`
	TenantID    string    `json:"tenant_id"`
	MediaID     string    `json:"media_id"`
	AssetID     string    `json:"asset_id,omitempty"`
	Traceparent string    `json:"traceparent,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// WebhookDeliveryEnvelope is the body queued for webhook-worker. The body
// payload is opaque bytes (canonical JSON of the customer-facing event).
type WebhookDeliveryEnvelope struct {
	DeliveryID  string    `json:"delivery_id"`
	TenantID    string    `json:"tenant_id"`
	MediaID     string    `json:"media_id"`
	EventID     string    `json:"event_id"`
	EventType   EventType `json:"event_type"`
	WebhookURL  string    `json:"webhook_url"`
	Payload     []byte    `json:"payload"`
	SecretKeyID string    `json:"secret_key_id,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// MediaCompletedPayload is the JSON body delivered to the customer's webhook
// endpoint on media.v1.completed. Stable public contract.
type MediaCompletedPayload struct {
	EventID   string           `json:"event_id"`
	EventType EventType        `json:"event_type"`
	TenantID  string           `json:"tenant_id"`
	MediaID   string           `json:"media_id"`
	MediaType string           `json:"media_type"`
	Lifecycle string           `json:"lifecycle"`
	Assets    []CompletedAsset `json:"assets"`
	CreatedAt time.Time        `json:"created_at"`
}

type CompletedAsset struct {
	AssetID     string `json:"asset_id"`
	Role        string `json:"role"`
	Kind        string `json:"kind"`
	Operation   string `json:"operation,omitempty"`
	ContentType string `json:"content_type"`
	Extension   string `json:"extension"`
	SizeBytes   uint64 `json:"size_bytes"`
	SHA256      string `json:"sha256,omitempty"`
}
