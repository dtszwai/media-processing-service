package admin

import (
	"context"
	"errors"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

const (
	tenantStateActive    = "ACTIVE"
	tenantStateSuspended = "SUSPENDED"
)

type TenantAdmin struct {
	KV       kv.KV
	Recorder auditapp.Recorder
	Now      func() time.Time
}

func NewTenantAdmin(k kv.KV, recorder auditapp.Recorder) *TenantAdmin {
	if recorder == nil {
		recorder = auditapp.NoopRecorder{}
	}
	return &TenantAdmin{KV: k, Recorder: recorder, Now: func() time.Time { return time.Now().UTC() }}
}

func (a *TenantAdmin) Suspend(ctx context.Context, tenantID, reason, actorUserID string) error {
	return a.setSuspension(ctx, tenantID, reason, actorUserID, true)
}

func (a *TenantAdmin) Unsuspend(ctx context.Context, tenantID, reason, actorUserID string) error {
	return a.setSuspension(ctx, tenantID, reason, actorUserID, false)
}

func (a *TenantAdmin) IsSuspended(ctx context.Context, tenantID string) (bool, error) {
	if tenantID == "" {
		return false, errors.New("tenant admin: tenant_id required")
	}
	var row struct {
		Status string `dynamodbav:"status"`
	}
	if err := a.KV.Get(ctx, tenantStateKey(tenantID), &row); err != nil {
		if errors.Is(err, kv.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	return row.Status == tenantStateSuspended, nil
}

func (a *TenantAdmin) setSuspension(ctx context.Context, tenantID, reason, actorUserID string, suspended bool) error {
	if tenantID == "" {
		return errors.New("tenant admin: tenant_id required")
	}
	if reason == "" {
		return errors.New("tenant admin: reason required")
	}
	now := a.Now().UTC()
	status := tenantStateActive
	ev := auditapp.NewAdminTenantUnsuspended(actorUserID, tenantID, reason, "")
	if suspended {
		status = tenantStateSuspended
		ev = auditapp.NewAdminTenantSuspended(actorUserID, tenantID, reason, "")
	}
	ev.ID = randid.New()
	ev.CreatedAt = now
	err := a.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                auditapp.BuildEventRow(ev),
			ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
		}},
		{Update: &kv.UpdateOp{
			Key:              tenantStateKey(tenantID),
			UpdateExpression: "SET item_type = :item_type, tenant_id = :tenant_id, #st = :status, reason_code = :reason, actor_user_id = :actor, updated_at = :now, created_at = if_not_exists(created_at, :now)",
			ExpressionAttributeNames: kv.Names{
				"#st": "status",
			},
			ExpressionAttributeValues: kv.Values{
				":item_type": "TENANT_STATE",
				":tenant_id": tenantID,
				":status":    status,
				":reason":    reason,
				":actor":     actorUserID,
				":now":       now.Format(time.RFC3339Nano),
			},
		}},
	})
	return err
}

func tenantStateKey(tenantID string) kv.Key {
	return kv.Key{PK: "TENANT#" + tenantID, SK: "STATE"}
}
