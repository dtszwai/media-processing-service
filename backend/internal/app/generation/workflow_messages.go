package generation

import (
	"encoding/json"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
)

// StageMessage is the ID-only outbox body for the next stage. The receiving
// worker re-fetches the Job (and decrypts prompts) at the repo boundary; no
// plaintext crosses the SNS/SQS boundary.
type StageMessage struct {
	TenantID      string                   `json:"tenant_id"`
	TenantLane    string                   `json:"tenant_lane"`
	JobID         string                   `json:"job_id"`
	Stage         generation.Stage         `json:"stage"`
	StageVersion  uint64                   `json:"stage_version"`
	ResourceClass generation.ResourceClass `json:"resource_class"`
	Traceparent   string                   `json:"traceparent,omitempty"`
}

func MarshalStageMessage(tenantID, jobID string, stage generation.Stage, version uint64, class generation.ResourceClass, traceparent string) ([]byte, error) {
	if version == 0 {
		version = 1
	}
	return json.Marshal(StageMessage{
		TenantID:      tenantID,
		TenantLane:    TenantLane(tenantID),
		JobID:         jobID,
		Stage:         stage,
		StageVersion:  version,
		ResourceClass: class,
		Traceparent:   traceparent,
	})
}

func UnmarshalStageMessage(body []byte) (StageMessage, error) {
	var msg StageMessage
	err := json.Unmarshal(body, &msg)
	return msg, err
}
