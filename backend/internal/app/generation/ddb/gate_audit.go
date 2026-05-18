package ddb

import (
	"time"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
)

// BuildGateAuditRow returns the per-job disclosure-gate row. JobRepo writes it
// inside AdvanceStageAndEnqueue so the decision is persisted in the same
// transaction as the stage transition it describes.
func BuildGateAuditRow(d genapp.GateDecision, now time.Time) map[string]any {
	return map[string]any{
		"PK":                 AuditGatePK(d.JobID),
		"SK":                 now.Format(time.RFC3339Nano),
		"tenant_id":          d.TenantID,
		"job_id":             d.JobID,
		"gate_version":       d.GateVersion,
		"output_type":        d.OutputType,
		"provider":           d.Provider,
		"model":              d.Model,
		"disclosure_present": d.DisclosurePresent,
		"watermark_present":  d.WatermarkPresent,
		"safety_present":     d.SafetyPresent,
		"decision":           d.Decision,
		"error_code":         d.ErrorCode,
		"ttl_epoch":          now.Add(365 * 24 * time.Hour).Unix(),
		"created_at":         now.Format(time.RFC3339Nano),
	}
}

// BuildGateAuditEventRow returns the audit-wide disclosure-gate row. It is
// separate from the per-job row because audit dashboards scan tenant/day
// partitions, while job detail screens read AUDIT#GATE by job id.
func BuildGateAuditEventRow(d genapp.GateDecision, now time.Time) map[string]any {
	ev := auditapp.NewSafetyDisclosureGateDecided(
		d.TenantID, d.JobID, d.Decision, d.ErrorCode,
		d.GateVersion, d.Provider, d.Model,
		d.WatermarkPresent, d.DisclosurePresent, d.SafetyPresent,
	)
	ev.ID = "gate#" + d.JobID
	ev.CreatedAt = now
	return auditapp.BuildEventRow(ev)
}
