package ops

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
)

func TestDecodeTenantUsageReservoirReadsAggregateRow(t *testing.T) {
	created := time.Date(2026, 5, 18, 8, 0, 0, 0, time.UTC)
	updated := created.Add(2 * time.Minute)
	av, err := attributevalue.MarshalMap(map[string]any{
		"PK":             "RESERVOIR#TENANT#tenant_local#COST_MICRO_USD#20260518",
		"SK":             "AGG",
		"scope_type":     "TENANT",
		"scope_id":       "tenant_local",
		"metric":         "COST_MICRO_USD",
		"period":         "20260518",
		"cap":            int64(5_000_000),
		"available":      int64(4_996_000),
		"reserved":       int64(0),
		"committed":      int64(4_000),
		"released":       int64(0),
		"state":          "OPEN",
		"policy_id":      "tenant_default_v1",
		"policy_version": int64(1),
		"created_at":     created.Format(time.RFC3339Nano),
		"updated_at":     updated.Format(time.RFC3339Nano),
	})
	if err != nil {
		t.Fatalf("marshal usage row: %v", err)
	}

	got, ok := decodeTenantUsageReservoir(av)
	if !ok {
		t.Fatal("decodeTenantUsageReservoir returned ok=false")
	}
	if got.TenantID != "tenant_local" || got.Metric != "COST_MICRO_USD" || got.Period != "20260518" {
		t.Fatalf("usage identity = %+v", got)
	}
	if got.Cap != 5_000_000 || got.Available != 4_996_000 || got.Committed != 4_000 {
		t.Fatalf("usage numbers = %+v", got)
	}
	if !got.Materialized {
		t.Fatal("Materialized = false, want true")
	}
	if !got.UpdatedAt.Equal(updated) {
		t.Fatalf("UpdatedAt = %s, want %s", got.UpdatedAt, updated)
	}
}

func TestTenantUsageViewSynthesizesUnopenedDailyCost(t *testing.T) {
	view := tenantUsageView("tenant_local", "20260518", 5_000_000, nil)

	if view.DailyCost == nil {
		t.Fatal("DailyCost is nil")
	}
	if view.DailyCost.Cap != 5_000_000 || view.DailyCost.Available != 5_000_000 || view.DailyCost.Committed != 0 {
		t.Fatalf("DailyCost = %+v", view.DailyCost)
	}
	if view.DailyCost.Materialized {
		t.Fatal("Materialized = true, want false for unopened reservoir")
	}
	if len(view.Reservoirs) != 0 {
		t.Fatalf("Reservoirs = %d, want 0 actual rows", len(view.Reservoirs))
	}
}

func TestTenantUsageViewPrefersMaterializedDailyCost(t *testing.T) {
	rows := []TenantUsageReservoir{
		{TenantID: "tenant_local", Metric: "REQUESTS", Period: "20260518", Cap: 10, Available: 8, Committed: 2, Materialized: true},
		{TenantID: "tenant_local", Metric: "COST_MICRO_USD", Period: "20260518", Cap: 5_000_000, Available: 4_975_000, Reserved: 4_000, Committed: 21_000, Materialized: true},
	}

	view := tenantUsageView("tenant_local", "20260518", 5_000_000, rows)

	if view.DailyCost == nil || !view.DailyCost.Materialized {
		t.Fatalf("DailyCost = %+v, want materialized row", view.DailyCost)
	}
	if view.DailyCost.Committed != 21_000 || view.DailyCost.Reserved != 4_000 {
		t.Fatalf("DailyCost numbers = %+v", view.DailyCost)
	}
}
