package admin

import (
	"context"
	"errors"
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	quotaapp "github.com/dtszwai/media-processing-service/backend/internal/app/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
)

type QuotaAdmin struct {
	Repo     *quotaapp.Repo
	Recorder auditapp.Recorder
	Now      func() time.Time
}

type QuotaOverrideResult struct {
	TenantID         string
	Metric           string
	Period           string
	PreviousCap      int64
	NewCap           int64
	ReservoirVersion int64
}

func NewQuotaAdmin(repo *quotaapp.Repo, recorder auditapp.Recorder) *QuotaAdmin {
	if recorder == nil {
		recorder = auditapp.NoopRecorder{}
	}
	return &QuotaAdmin{Repo: repo, Recorder: recorder, Now: func() time.Time { return time.Now().UTC() }}
}

func (a *QuotaAdmin) OverrideTenantCap(ctx context.Context, tenantID, metric, period string, newCap int64, reason, actorUserID string) (QuotaOverrideResult, error) {
	if a == nil || a.Repo == nil {
		return QuotaOverrideResult{}, errors.New("quota admin: repo required")
	}
	if tenantID == "" || metric == "" || period == "" {
		return QuotaOverrideResult{}, errors.New("quota admin: tenant_id, metric, and period required")
	}
	if newCap < 0 {
		return QuotaOverrideResult{}, errors.New("quota admin: new_cap must be non-negative")
	}
	if reason == "" {
		return QuotaOverrideResult{}, errors.New("quota admin: reason required")
	}
	result, err := a.Repo.OverrideCap(ctx, quota.ScopeTenant, tenantID, quota.Metric(metric), period, newCap)
	if err != nil {
		return QuotaOverrideResult{}, err
	}
	if err := a.Recorder.Record(ctx, auditapp.NewQuotaCapChanged(actorUserID, string(quota.ScopeTenant), tenantID, metric, period, result.PreviousCap, newCap, "")); err != nil {
		return QuotaOverrideResult{}, err
	}
	return QuotaOverrideResult{
		TenantID:         tenantID,
		Metric:           metric,
		Period:           period,
		PreviousCap:      result.PreviousCap,
		NewCap:           newCap,
		ReservoirVersion: result.ReservoirVersion,
	}, nil
}
