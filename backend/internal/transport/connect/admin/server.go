// Package admin adapts app/admin onto the Connect transport surface.
package admin

import (
	adminapp "github.com/dtszwai/media-processing-service/backend/internal/app/admin"
)

// Config bundles the admin-application dependencies onto their Connect surfaces.
type Config struct {
	DLQ         *adminapp.DLQAdmin
	OutboxDLQ   *adminapp.OutboxDLQAdmin
	Idempotency *adminapp.IdempotencyAdmin
	Quota       *adminapp.QuotaAdmin
	Webhook     *adminapp.WebhookAdmin
	Tenant      *adminapp.TenantAdmin
	Workflow    *adminapp.WorkflowAdmin
}

type Server struct {
	dlq         *adminapp.DLQAdmin
	outboxDLQ   *adminapp.OutboxDLQAdmin
	idempotency *adminapp.IdempotencyAdmin
	quota       *adminapp.QuotaAdmin
	webhook     *adminapp.WebhookAdmin
	tenant      *adminapp.TenantAdmin
	workflow    *adminapp.WorkflowAdmin
}

func NewServer(cfg Config) *Server {
	return &Server{
		dlq:         cfg.DLQ,
		outboxDLQ:   cfg.OutboxDLQ,
		idempotency: cfg.Idempotency,
		quota:       cfg.Quota,
		webhook:     cfg.Webhook,
		tenant:      cfg.Tenant,
		workflow:    cfg.Workflow,
	}
}
