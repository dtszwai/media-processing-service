package ops

import (
	"context"
	"sort"
	"strings"

	gendb "github.com/dtszwai/media-processing-service/backend/internal/app/generation/ddb"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/postprocess"
)

func (s *Service) gateDecision(ctx context.Context, jobID string, view *FullJobView) (*GateDecisionView, error) {
	rows, err := s.queryAll(ctx, kv.QueryRequest{
		KeyConditionExpression:    "PK = :pk",
		ExpressionAttributeValues: kv.Values{":pk": gendb.AuditGatePK(jobID)},
	})
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	sort.Slice(rows, func(i, j int) bool {
		return stringAttr(rows[i], "created_at") > stringAttr(rows[j], "created_at")
	})
	row := rows[0]
	out := &GateDecisionView{
		JobID:             stringAttr(row, "job_id"),
		TenantID:          stringAttr(row, "tenant_id"),
		GateVersion:       stringAttr(row, "gate_version"),
		OutputType:        stringAttr(row, "output_type"),
		Provider:          stringAttr(row, "provider"),
		Model:             stringAttr(row, "model"),
		Decision:          stringAttr(row, "decision"),
		ErrorCode:         stringAttr(row, "error_code"),
		WatermarkPresent:  boolAttr(row, "watermark_present"),
		DisclosurePresent: boolAttr(row, "disclosure_present"),
		SafetyPresent:     boolAttr(row, "safety_present"),
		DecidedAt:         timeAttr(row, "created_at"),
	}
	auditPK := gendb.AuditGatePK(jobID)
	auditSK := stringAttr(row, "SK")
	if !isDisclosureGateAudit(out) {
		view.Spans = append(view.Spans, terminalAuditSpan(out, auditPK, auditSK))
		return nil, nil
	}
	view.Spans = append(view.Spans, disclosureGateSpan(out, auditPK, auditSK))
	return out, nil
}

func disclosureGateSpan(out *GateDecisionView, pk, sk string) TraceSpan {
	return TraceSpan{
		ID:       "gate",
		Kind:     "GATE_AUDIT",
		Label:    "gate decision · " + out.Decision,
		Status:   gateDecisionStatus(out.Decision),
		Stage:    string(generation.StageDisclosurePostprocess),
		ParentID: "stage:" + string(generation.StageDisclosurePostprocess),
		StartAt:  out.DecidedAt,
		EndAt:    out.DecidedAt,
		Attributes: map[string]string{
			"decision":           out.Decision,
			"watermark_present":  boolStr(out.WatermarkPresent),
			"disclosure_present": boolStr(out.DisclosurePresent),
			"safety_present":     boolStr(out.SafetyPresent),
		},
		PK: pk,
		SK: sk,
	}
}

func terminalAuditSpan(out *GateDecisionView, pk, sk string) TraceSpan {
	return TraceSpan{
		ID:        "terminal-audit",
		Kind:      "TERMINAL_AUDIT",
		Label:     "failure audit · " + out.Decision,
		Status:    gateDecisionStatus(out.Decision),
		Stage:     string(generation.StageTerminal),
		ErrorCode: out.ErrorCode,
		StartAt:   out.DecidedAt,
		EndAt:     out.DecidedAt,
		Attributes: map[string]string{
			"decision":   out.Decision,
			"error_code": out.ErrorCode,
		},
		PK: pk,
		SK: sk,
	}
}

func isDisclosureGateAudit(out *GateDecisionView) bool {
	if out.Decision == "PASS" {
		return true
	}
	switch out.ErrorCode {
	case "WATERMARK_FINGERPRINT_MISSING",
		"WATERMARK_ALGO_MISMATCH",
		"WATERMARK_MISSING",
		"WATERMARK_OR_DISCLOSURE_MISSING",
		"AI_DISCLOSURE_MISSING",
		"OUTPUT_SAFETY_MISSING":
		return true
	default:
		return false
	}
}

func gateDecisionStatus(decision string) string {
	if decision == "PASS" {
		return "OK"
	}
	return "TERMINAL_FAIL"
}

func boolStr(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

func (s *Service) enrichWatermark(ctx context.Context, asset map[string]any, dec *GateDecisionView) {
	if s.Blob == nil {
		return
	}
	key, _ := asset["storage_key"].(string)
	if key == "" {
		return
	}
	meta, err := s.Blob.HeadMetadata(ctx, key)
	if err != nil {
		return
	}
	for k, v := range meta {
		switch strings.ToLower(k) {
		case postprocess.MetadataKeys.Fingerprint:
			dec.WatermarkFingerprint = v
		case postprocess.MetadataKeys.Algo:
			dec.WatermarkAlgo = v
		case postprocess.MetadataKeys.Position:
			dec.WatermarkPosition = v
		case postprocess.MetadataKeys.Text:
			dec.WatermarkText = v
		}
	}
}
