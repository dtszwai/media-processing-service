package ops

import (
	"encoding/json"
	"fmt"
	"time"

	connect "connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	opsapp "github.com/dtszwai/media-processing-service/backend/internal/app/ops"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/pbutil"
	opsv1 "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/ops/v1"
)

func jobSummaryToProto(r opsapp.JobSummary) *opsv1.JobSummary {
	return &opsv1.JobSummary{
		JobId:        r.JobID,
		TenantId:     r.TenantID,
		MediaId:      r.MediaID,
		Status:       r.Status,
		CurrentStage: r.CurrentStage,
		OutputType:   r.OutputType,
		Tier:         r.Tier,
		Model:        r.Model,
		Attempts:     r.Attempts,
		ErrorCode:    r.ErrorCode,
		CreatedAt:    protoutil.OptTimestamp(r.CreatedAt),
		UpdatedAt:    protoutil.OptTimestamp(r.UpdatedAt),
		CompletedAt:  protoutil.OptTimestampPtr(r.CompletedAt),
	}
}

func mediaRowToProto(r opsapp.MediaRow) *opsv1.MediaRow {
	out := &opsv1.MediaRow{
		MediaId:     r.MediaID,
		TenantId:    r.TenantID,
		OwnerUserId: r.OwnerUserID,
		Origin:      r.Origin,
		MediaType:   r.MediaType,
		Lifecycle:   r.Lifecycle,
		CreatedAt:   protoutil.OptTimestamp(r.CreatedAt),
		UpdatedAt:   protoutil.OptTimestamp(r.UpdatedAt),
		DeletedAt:   protoutil.OptTimestampPtr(r.DeletedAt),
	}
	if r.OriginalAssetID != "" {
		out.OriginalAssetId = &r.OriginalAssetID
	}
	if r.JobID != "" {
		out.JobId = &r.JobID
	}
	return out
}

func ddbRowToProto(r opsapp.DdbRow) *opsv1.DdbRow {
	return &opsv1.DdbRow{
		Pk:         r.PK,
		Sk:         r.SK,
		ItemType:   r.ItemType,
		Attributes: mapToStruct(r.Attributes),
	}
}

func traceSpanToProto(s opsapp.TraceSpan) *opsv1.TraceSpan {
	return &opsv1.TraceSpan{
		Id:            s.ID,
		ParentId:      s.ParentID,
		Kind:          s.Kind,
		Label:         s.Label,
		Status:        s.Status,
		Stage:         s.Stage,
		ResourceClass: s.ResourceClass,
		AttemptNo:     s.AttemptNo,
		ErrorCode:     s.ErrorCode,
		ErrorMessage:  s.ErrorMessage,
		Attributes:    s.Attributes,
		StartAt:       protoutil.OptTimestamp(s.StartAt),
		EndAt:         protoutil.OptTimestamp(s.EndAt),
		DurationMs:    s.DurationMS,
		Pk:            s.PK,
		Sk:            s.SK,
	}
}

func gateDecisionToProto(d *opsapp.GateDecisionView) *opsv1.GateDecision {
	return &opsv1.GateDecision{
		JobId:                d.JobID,
		TenantId:             d.TenantID,
		GateVersion:          d.GateVersion,
		OutputType:           d.OutputType,
		Provider:             d.Provider,
		Model:                d.Model,
		Decision:             d.Decision,
		ErrorCode:            d.ErrorCode,
		WatermarkPresent:     d.WatermarkPresent,
		DisclosurePresent:    d.DisclosurePresent,
		SafetyPresent:        d.SafetyPresent,
		WatermarkFingerprint: d.WatermarkFingerprint,
		WatermarkAlgo:        d.WatermarkAlgo,
		WatermarkPosition:    d.WatermarkPosition,
		WatermarkText:        d.WatermarkText,
		DecidedAt:            protoutil.OptTimestamp(d.DecidedAt),
	}
}

// mapToStruct converts a raw row into a google.protobuf.Struct. Non-JSON
// types are converted to strings so the console can still render the row.
func mapToStruct(row map[string]any) *structpb.Struct {
	if len(row) == 0 {
		return nil
	}
	normalised := normaliseForStruct(row).(map[string]any)
	out, err := structpb.NewStruct(normalised)
	if err != nil {
		raw, _ := json.Marshal(row)
		var parsed map[string]any
		_ = json.Unmarshal(raw, &parsed)
		out, _ = structpb.NewStruct(parsed)
	}
	return out
}

func normaliseForStruct(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = normaliseForStruct(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = normaliseForStruct(vv)
		}
		return out
	case []byte:
		return fmt.Sprintf("<bytes:%dB>", len(t))
	case time.Time:
		return t.UTC().Format(time.RFC3339Nano)
	case int:
		return float64(t)
	case int32:
		return float64(t)
	case int64:
		return float64(t)
	case uint64:
		return float64(t)
	case float32:
		return float64(t)
	case nil, string, bool, float64:
		return t
	}
	return fmt.Sprintf("%v", v)
}

func toConnect(err error) error {
	if err == nil {
		return nil
	}
	if connect.CodeOf(err) != connect.CodeUnknown {
		return err
	}
	return connect.NewError(connect.CodeInternal, err)
}
