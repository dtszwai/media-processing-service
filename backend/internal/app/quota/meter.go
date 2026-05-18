package quota

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
)

// Default reservoir caps for each (scope, metric) tuple the Meter drives.
// These are the v1 defaults applied at Ensure time; OverrideCap mutates the
// live row independently. Bytes are exact, counts are calls-per-period.
const (
	defaultTenantRequestCapPerDay   int64 = 10_000_000     // tenant calls per day
	defaultAPIKeyRequestCapPerDay   int64 = 1_000_000      // calls per api-key per day
	defaultTenantStorageBytesCap    int64 = 1 << 50        // bytes per tenant per month (~1 PiB)
	defaultTenantGeneratedOutputCap int64 = 100_000        // generated outputs per tenant per day
	defaultVendorCostCapPerDay      int64 = 1_000_000_000  // micro-USD per vendor per day
	defaultServiceCostCapPerDay     int64 = 10_000_000_000 // micro-USD service-global per day
)

// Reason strings stamped on each ledger row. Named so call-sites that
// dashboard or alert on `reason` can switch on a constant rather than a
// stringly-typed literal.
const (
	reasonTenantRequestPrefix = "TENANT_REQUEST:"
	reasonAPIKeyRequestPrefix = "API_KEY_REQUEST:"
	reasonStorageBytes        = "STORAGE_BYTES"
	reasonGeneratedOutput     = "GENERATED_OUTPUT"
	reasonVendorCost          = "VENDOR_COST"
	reasonServiceCostPrefix   = "SERVICE_COST:"
)

// Meter is the scope-agnostic call-site entrypoint that pins the cap policy
// and exposes per-resource RecordX helpers. Each helper translates its
// call-site vocabulary (vendor cost, tenant request, storage bytes…) into a
// Reservoir (scope, metric, period) tuple and runs reserve + commit against
// the underlying Repo.
type Meter struct {
	repo          *Repo
	policyID      string
	policyVersion int64
	now           func() time.Time
}

func NewMeter(repo *Repo) *Meter {
	return &Meter{
		repo:          repo,
		policyID:      "default_quota_v1",
		policyVersion: 1,
		now:           func() time.Time { return time.Now().UTC() },
	}
}

func (m *Meter) RecordRequest(ctx context.Context, tenantID, userID, apiKeyID, routeClass, reservationID string) error {
	if m == nil || tenantID == "" {
		return nil
	}
	period := quota.PeriodDaily(m.now())
	if err := m.reserveCommit(ctx, quota.ScopeTenant, tenantID, quota.Requests, period, "tenant-request:"+reservationID, 1, reasonTenantRequestPrefix+routeClass, defaultTenantRequestCapPerDay); err != nil {
		return err
	}
	if apiKeyID != "" {
		return m.reserveCommit(ctx, quota.ScopeAPIKey, apiKeyID, quota.Requests, period, "api-key-request:"+reservationID, 1, reasonAPIKeyRequestPrefix+routeClass, defaultAPIKeyRequestCapPerDay)
	}
	return nil
}

func (m *Meter) RecordStorageBytes(ctx context.Context, tenantID, mediaID, assetID string, bytes int64) error {
	if m == nil || tenantID == "" || bytes <= 0 {
		return nil
	}
	return m.reserveCommit(ctx, quota.ScopeTenant, tenantID, quota.StorageBytes, quota.PeriodMonthly(m.now()), "storage:"+mediaID+":"+assetID, bytes, reasonStorageBytes, defaultTenantStorageBytesCap)
}

func (m *Meter) RecordGeneratedOutput(ctx context.Context, tenantID, jobID, assetID string) error {
	if m == nil || tenantID == "" {
		return nil
	}
	return m.reserveCommit(ctx, quota.ScopeTenant, tenantID, quota.GeneratedOutputs, quota.PeriodDaily(m.now()), "generated-output:"+jobID+":"+assetID, 1, reasonGeneratedOutput, defaultTenantGeneratedOutputCap)
}

func (m *Meter) RecordVendorCost(ctx context.Context, vendor, jobID string, microUSD int64) error {
	if m == nil || vendor == "" || microUSD <= 0 {
		return nil
	}
	return m.reserveCommit(ctx, quota.ScopeVendor, strings.ToUpper(vendor), quota.CostMicroUSD, quota.PeriodDaily(m.now()), "vendor-cost:"+jobID, microUSD, reasonVendorCost, defaultVendorCostCapPerDay)
}

func (m *Meter) RecordServiceCost(ctx context.Context, jobID, source, requestID string, microUSD int64) error {
	if m == nil || microUSD <= 0 {
		return nil
	}
	if source == "" {
		source = "unknown"
	}
	if requestID == "" {
		requestID = "unknown"
	}
	return m.reserveCommit(ctx, quota.ScopeService, "service.global", quota.CostMicroUSD, quota.PeriodDaily(m.now()), "service-cost:"+jobID+":"+source+":"+requestID, microUSD, reasonServiceCostPrefix+source, defaultServiceCostCapPerDay)
}

func (m *Meter) reserveCommit(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period, reservationID string, amount int64, reason string, capN int64) error {
	if err := m.repo.Ensure(ctx, scope, scopeID, metric, period, capN, m.policyID, m.policyVersion); err != nil {
		return err
	}
	now := m.now()
	res := quota.Reservation{
		ID:             reservationID,
		Amount:         amount,
		State:          quota.ReservationReserved,
		Reason:         reason,
		PricingVersion: fmt.Sprintf("%s#%d", m.policyID, m.policyVersion),
		CreatedAt:      now,
		ReservedAt:     now,
	}
	if err := m.repo.Reserve(ctx, scope, scopeID, metric, period, res); err != nil {
		return err
	}
	return m.repo.Commit(ctx, scope, scopeID, metric, period, reservationID, amount)
}
