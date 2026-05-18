package admin

import (
	"context"
	"errors"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/app/webhook"
)

type WebhookAdmin struct {
	Secrets  *webhook.SecretStore
	Recorder auditapp.Recorder
}

func NewWebhookAdmin(secrets *webhook.SecretStore, recorder auditapp.Recorder) *WebhookAdmin {
	if recorder == nil {
		recorder = auditapp.NoopRecorder{}
	}
	return &WebhookAdmin{Secrets: secrets, Recorder: recorder}
}

func (a *WebhookAdmin) RotateSecret(ctx context.Context, tenantID, endpointID, reason, actorUserID string) (oldKeyID, newKeyID string, err error) {
	if a == nil || a.Secrets == nil {
		return "", "", errors.New("webhook admin: secret store required")
	}
	if tenantID == "" {
		return "", "", errors.New("webhook admin: tenant_id required")
	}
	if endpointID == "" {
		endpointID = "tenant-default"
	}
	if reason == "" {
		return "", "", errors.New("webhook admin: reason required")
	}
	oldKeyID, newKeyID, err = a.Secrets.Rotate(ctx, tenantID)
	if err != nil {
		return "", "", err
	}
	if err := a.Recorder.Record(ctx, auditapp.NewWebhookSecretRotated(actorUserID, tenantID, endpointID, oldKeyID, newKeyID, "")); err != nil {
		return "", "", err
	}
	return oldKeyID, newKeyID, nil
}
