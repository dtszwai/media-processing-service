package app

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	clearEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Env != "local" {
		t.Errorf("env: got %q, want %q", cfg.Env, "local")
	}
	if cfg.AWS.Region != "us-east-1" {
		t.Errorf("aws.region: got %q", cfg.AWS.Region)
	}
	if cfg.AWS.DDBTable != "media-v1" {
		t.Errorf("aws.ddb_table: got %q", cfg.AWS.DDBTable)
	}
	if cfg.API.Addr != ":9000" {
		t.Errorf("api.addr: got %q", cfg.API.Addr)
	}
	if cfg.API.AuthEnforcement {
		t.Errorf("api.auth_enforcement: got true, want false")
	}
	if cfg.Quota.TenantCostCapMicroUSD != 5_000_000 {
		t.Errorf("quota.tenant_cost_cap_micro_usd: got %d", cfg.Quota.TenantCostCapMicroUSD)
	}
	if cfg.Telemetry.LogLevel != "info" {
		t.Errorf("telemetry.log_level: got %q", cfg.Telemetry.LogLevel)
	}
	if cfg.Telemetry.TracesSampler != 1.0 {
		t.Errorf("telemetry.traces_sampler: got %v", cfg.Telemetry.TracesSampler)
	}
}

func TestLoad_EnvOverridesDefaults(t *testing.T) {
	clearEnv(t)
	t.Setenv("DDB_TABLE", "my-custom-table")
	t.Setenv("QUOTA_TENANT_COST_CAP_MICRO_USD", "12345")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("OTEL_TRACES_SAMPLER_ARG", "0.25")
	t.Setenv("AUTH_ENFORCEMENT_ENABLED", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AWS.DDBTable != "my-custom-table" {
		t.Errorf("aws.ddb_table: got %q", cfg.AWS.DDBTable)
	}
	if cfg.Quota.TenantCostCapMicroUSD != 12345 {
		t.Errorf("quota.tenant_cost_cap_micro_usd: got %d", cfg.Quota.TenantCostCapMicroUSD)
	}
	if cfg.Telemetry.LogLevel != "debug" {
		t.Errorf("telemetry.log_level: got %q", cfg.Telemetry.LogLevel)
	}
	if cfg.Telemetry.TracesSampler != 0.25 {
		t.Errorf("telemetry.traces_sampler: got %v", cfg.Telemetry.TracesSampler)
	}
	if !cfg.API.AuthEnforcement {
		t.Errorf("api.auth_enforcement: env=true should win")
	}
}

func TestLoad_LeaseReaperTenantsCommaSeparated(t *testing.T) {
	clearEnv(t)
	t.Setenv("LEASE_REAPER_TENANTS", "tenant-a,tenant-b,tenant-c")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := []string{"tenant-a", "tenant-b", "tenant-c"}
	if len(cfg.LeaseReaper.Tenants) != len(want) {
		t.Fatalf("tenants count: got %d, want %d (got=%v)", len(cfg.LeaseReaper.Tenants), len(want), cfg.LeaseReaper.Tenants)
	}
	for i, v := range want {
		if cfg.LeaseReaper.Tenants[i] != v {
			t.Errorf("tenants[%d]: got %q, want %q", i, cfg.LeaseReaper.Tenants[i], v)
		}
	}
}

// clearEnv unsets every variable Load() inspects so test order doesn't matter
// and t.Setenv (which auto-restores) can layer cleanly.
func clearEnv(t *testing.T) {
	t.Helper()
	vars := []string{
		"MSG_ENV",
		"AWS_REGION", "AWS_ENDPOINT_URL",
		"DDB_TABLE", "S3_BUCKET",
		"SNS_MEDIA_TOPIC", "SNS_MEDIA_CLEANUP_TOPIC",
		"SNS_GENERATION_TOPIC", "SNS_ANALYTICS_TOPIC",
		"SQS_MEDIA_QUEUE", "SQS_MEDIA_CLEANUP_QUEUE",
		"SQS_ANALYTICS_QUEUE", "SQS_WEBHOOK_QUEUE",
		"API_HTTP_ADDR", "CORS_ALLOWED_ORIGINS",
		"AUTH_ENFORCEMENT_ENABLED",
		"NOTEBOOKLM_SCRIPT_PATH", "NOTEBOOKLM_STORAGE_STATE_PATH", "NOTEBOOKLM_STORAGE_STATE_DISPLAY_PATH",
		"NOTEBOOKLM_PYTHON",
		"QUOTA_TENANT_COST_CAP_MICRO_USD",
		"LEASE_REAPER_TENANTS",
		"LOG_LEVEL", "OTEL_TRACES_SAMPLER_ARG",
		"OTEL_LOGS_DISABLED", "OTEL_EXPORTER_OTLP_ENDPOINT",
	}
	for _, k := range vars {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
}
