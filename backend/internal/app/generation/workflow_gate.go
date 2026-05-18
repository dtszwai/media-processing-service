package generation

import (
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// GateDecision captures the per-output-type gate verdict. Used both for
// VerifyPublishableArtifact's return and for the audit row.
type GateDecision struct {
	JobID             string
	TenantID          string
	GateVersion       string
	OutputType        string
	Provider          string
	Model             string
	DisclosurePresent bool
	WatermarkPresent  bool
	SafetyPresent     bool
	Decision          string // "PASS" or "FAIL"
	ErrorCode         string
	Timestamp         time.Time
}

func buildGateDecision(job *generation.Job, art generation.Artifact, ts time.Time) GateDecision {
	m := art.Metadata
	if m == nil {
		m = map[string]string{}
	}
	return GateDecision{
		JobID:             job.ID,
		TenantID:          job.TenantID,
		GateVersion:       "v1",
		OutputType:        string(job.OutputType),
		Provider:          m["provider"],
		Model:             m["model"],
		DisclosurePresent: m["disclosure"] != "",
		WatermarkPresent:  m["visible_watermark"] != "",
		SafetyPresent:     m["content_safety"] != "",
		Timestamp:         ts,
	}
}
