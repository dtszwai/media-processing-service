package ops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	gendb "github.com/dtszwai/media-processing-service/backend/internal/app/generation/ddb"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

const (
	maxListLimit     = 200
	defaultListLimit = 50
)

// ListJobs scans the base table for item_type=GEN rows. The console is a
// local-only inspector — a strict tenant-scoped Query would require knowing
// every status × tenant ahead of time, defeating the "show me everything"
// purpose of the tab.
func (s *Service) ListJobs(ctx context.Context, f ListJobsFilter) ([]JobSummary, string, error) {
	if s.DDB == nil {
		return nil, "", fmt.Errorf("ops: ddb client not wired")
	}
	in := &dynamodb.ScanInput{
		TableName:        aws.String(s.Table),
		FilterExpression: aws.String("item_type = :t"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":t": &types.AttributeValueMemberS{Value: "GEN"},
		},
	}
	rows, cursor, err := scanUntilLimit(ctx, s.DDB, in, f.Cursor, f.Limit, f.decodeJobSummary)
	if err != nil {
		return nil, "", err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
	return rows, cursor, nil
}

func (f ListJobsFilter) decodeJobSummary(av map[string]types.AttributeValue) (JobSummary, bool) {
	row, ok := decodeJobSummary(av)
	if !ok || !f.matches(row) {
		return JobSummary{}, false
	}
	return row, true
}

func (f ListJobsFilter) matches(row JobSummary) bool {
	return matchesOptional(f.TenantID, row.TenantID) &&
		matchesOptional(f.Status, row.Status) &&
		matchesOptional(f.OutputType, row.OutputType)
}

func decodeJobSummary(av map[string]types.AttributeValue) (JobSummary, bool) {
	pk, _ := av["PK"].(*types.AttributeValueMemberS)
	sk, _ := av["SK"].(*types.AttributeValueMemberS)
	if pk == nil || sk == nil || !strings.HasPrefix(pk.Value, "JOB#") || sk.Value != "JOB" {
		return JobSummary{}, false
	}
	var raw struct {
		ID           string     `dynamodbav:"id"`
		TenantID     string     `dynamodbav:"tenant_id"`
		MediaID      string     `dynamodbav:"media_id,omitempty"`
		Status       string     `dynamodbav:"status"`
		CurrentStage string     `dynamodbav:"current_stage"`
		OutputType   string     `dynamodbav:"output_type"`
		Tier         string     `dynamodbav:"tier"`
		Model        string     `dynamodbav:"model,omitempty"`
		Attempts     int        `dynamodbav:"attempts,omitempty"`
		ErrorCode    string     `dynamodbav:"error_code,omitempty"`
		CreatedAt    time.Time  `dynamodbav:"created_at"`
		UpdatedAt    time.Time  `dynamodbav:"updated_at"`
		CompletedAt  *time.Time `dynamodbav:"completed_at,omitempty"`
	}
	if err := attributevalue.UnmarshalMap(av, &raw); err != nil {
		return JobSummary{}, false
	}
	out := JobSummary{
		JobID:        raw.ID,
		TenantID:     raw.TenantID,
		MediaID:      raw.MediaID,
		Status:       raw.Status,
		CurrentStage: raw.CurrentStage,
		OutputType:   raw.OutputType,
		Tier:         raw.Tier,
		Model:        raw.Model,
		Attempts:     int32(raw.Attempts),
		ErrorCode:    raw.ErrorCode,
		CreatedAt:    raw.CreatedAt,
		UpdatedAt:    raw.UpdatedAt,
		CompletedAt:  raw.CompletedAt,
	}
	return out, true
}

// GetJob returns the full operator view for a single job. The view is
// assembled from one Query against PK=JOB#<id>, a follow-up Get against the
// AUDIT#GATE partition, joins against the result Media + Asset rows, and a
// HeadObject against the final S3 asset for the watermark fingerprint. The
// helper functions sit in this file so the spans-from-rows mapping is one
// readable block.
func (s *Service) GetJob(ctx context.Context, jobID string) (*FullJobView, error) {
	if jobID == "" {
		return nil, fmt.Errorf("ops: job_id required")
	}
	rows, err := s.queryAll(ctx, kv.QueryRequest{
		KeyConditionExpression:    "PK = :pk",
		ExpressionAttributeValues: kv.Values{":pk": gendb.JobPK(jobID)},
		ConsistentRead:            true,
	})
	if err != nil {
		return nil, fmt.Errorf("ops: query job partition: %w", err)
	}
	view := &FullJobView{
		JobAttrs: map[string]any{},
		Spans:    []TraceSpan{},
	}
	for _, row := range rows {
		switch itemType(row) {
		case "GEN":
			view.JobAttrs = row
			view.Summary = summaryFromAttrs(row)
		case "STAGE_ATTEMPT":
			view.Spans = append(view.Spans, attemptSpan(row))
		case "PROVIDER_REQUEST":
			view.Spans = append(view.Spans, providerSpan(row))
		case "TERMINAL":
			view.Spans = append(view.Spans, terminalSpan(row))
		case "OUTPUT":
			view.Spans = append(view.Spans, outputSpan(row))
		case "VARIANT":
			view.Spans = append(view.Spans, variantSpan(row))
		}
	}
	// Roll spans up into stage groups: one STAGE span per distinct
	// (stage, version) pair the worker advanced through. Span IDs let the
	// console draw parent/child indentation without server-side recursion.
	view.Spans = append(view.Spans, deriveStageSpans(view.Spans)...)
	// For terminal jobs, the tail stage (PUBLISH) has no successor row to
	// derive its end_at from, and the TERMINAL audit row is written within
	// the same transaction so its timestamp can fall microseconds before
	// the stage's start_at — too early to qualify as a "closing event
	// after stage start". Without a proper anchor, closeStageEnds would
	// fall to s.now(), and s.now() ticks forward on every read — making
	// the stage bar appear to grow long after the job ended. Use the
	// job's authoritative CompletedAt instead.
	asOf := s.now()
	if view.Summary.CompletedAt != nil && !view.Summary.CompletedAt.IsZero() {
		asOf = *view.Summary.CompletedAt
	}
	if extra, ok := runningStageSpan(view.Summary, view.Spans, asOf); ok {
		view.Spans = append(view.Spans, extra)
	}
	closeStageEnds(view.Spans, asOf)
	linkChildrenToStages(view.Spans)

	// Gate decision lives on its own PK so it is one extra Query. Failures
	// surface via the logger — the view still renders without it, but the
	// operator should not be deceived into thinking no gate ever fired.
	dec, derr := s.gateDecision(ctx, jobID, view)
	if derr != nil && s.Logger != nil {
		s.Logger.WarnContext(ctx, "ops: gate decision read failed", "job_id", jobID, "err", derr)
	}
	if dec != nil {
		view.GateDecision = dec
	}

	// Joined Media + Asset rows — surfaced for the trace header so the
	// operator can deep-link to /ddb/:pk/:sk for either row.
	if view.Summary.TenantID != "" && view.Summary.MediaID != "" {
		if media, merr := s.fetchMediaRow(ctx, view.Summary.TenantID, view.Summary.MediaID); merr == nil {
			view.MediaAttrs = media
		}
		if assetID, _ := view.JobAttrs["result_asset_id"].(string); assetID != "" {
			if asset, aerr := s.fetchAssetRow(ctx, view.Summary.TenantID, view.Summary.MediaID, assetID); aerr == nil {
				view.AssetAttrs = asset
				if view.GateDecision != nil {
					s.enrichWatermark(ctx, asset, view.GateDecision)
				}
			}
		}
	}

	// Decrypt the prompt for the trace header. We piggyback on JobRepo
	// which already binds the KMS Sealer with the right EncryptionContext
	// (tenant_id + job_id) — same call site the workflow uses, so the
	// CloudTrail decrypt event reads identically whether the worker or
	// the operator triggered it. Failures are non-fatal: the trace
	// renders without the plaintext if KMS rejects or the sealer is not
	// wired (in-memory bring-up).
	if s.JobRepo != nil && view.Summary.TenantID != "" {
		j, jerr := s.JobRepo.GetJob(ctx, view.Summary.TenantID, jobID)
		if jerr == nil && j != nil {
			view.DecryptedPrompt = j.Prompt
			view.DecryptedPreparedPrompt = j.PreparedPrompt
		} else if jerr != nil && s.Logger != nil {
			s.Logger.WarnContext(ctx, "ops: prompt decrypt failed", "job_id", jobID, "err", jerr)
		}
	}

	// Sort all spans by start_at; stable so attempts within a stage keep
	// their attempt_no order when timestamps are identical.
	sort.SliceStable(view.Spans, func(i, j int) bool {
		return view.Spans[i].StartAt.Before(view.Spans[j].StartAt)
	})
	if len(view.Spans) > 0 {
		view.FirstEventAt = view.Spans[0].StartAt
		view.LastEventAt = view.Spans[len(view.Spans)-1].EndAt
		if view.LastEventAt.IsZero() {
			view.LastEventAt = view.Spans[len(view.Spans)-1].StartAt
		}
		// Authoritative upper bound for terminal jobs. Any span whose
		// EndAt reads beyond CompletedAt is by definition stale or
		// computed-on-read (see closeStageEnds note above); the trace
		// window must not lie about the job's actual wall-clock span.
		if view.Summary.CompletedAt != nil && !view.Summary.CompletedAt.IsZero() &&
			view.LastEventAt.After(*view.Summary.CompletedAt) {
			view.LastEventAt = *view.Summary.CompletedAt
		}
	}
	view.RelatedKeys = relatedKeysFor(jobID, view)
	return view, nil
}

// queryAll pages through a Query and returns every row. The console's
// trace tab needs the full partition; partial pages would draw a partial
// waterfall.
func (s *Service) queryAll(ctx context.Context, req kv.QueryRequest) ([]map[string]any, error) {
	out := []map[string]any{}
	for {
		page, err := s.KV.Query(ctx, req)
		if err != nil {
			return nil, err
		}
		for _, r := range page.Items {
			row := map[string]any{}
			if err := r.Unmarshal(&row); err != nil {
				continue
			}
			out = append(out, row)
		}
		if page.LastEvaluatedKey == nil {
			return out, nil
		}
		req.ExclusiveStartKey = page.LastEvaluatedKey
	}
}

func itemType(row map[string]any) string {
	if v, ok := row["item_type"].(string); ok {
		return v
	}
	return ""
}

func summaryFromAttrs(row map[string]any) JobSummary {
	out := JobSummary{}
	out.JobID = stringAttr(row, "id")
	out.TenantID = stringAttr(row, "tenant_id")
	out.MediaID = stringAttr(row, "media_id")
	out.Status = stringAttr(row, "status")
	out.CurrentStage = stringAttr(row, "current_stage")
	out.OutputType = stringAttr(row, "output_type")
	out.Tier = stringAttr(row, "tier")
	out.Model = stringAttr(row, "model")
	out.Attempts = int32(intAttr(row, "attempts"))
	out.ErrorCode = stringAttr(row, "error_code")
	out.CreatedAt = timeAttr(row, "created_at")
	out.UpdatedAt = timeAttr(row, "updated_at")
	if t := timeAttr(row, "completed_at"); !t.IsZero() {
		out.CompletedAt = &t
	}
	return out
}

func (s *Service) fetchMediaRow(ctx context.Context, tenantID, mediaID string) (map[string]any, error) {
	var row map[string]any
	if err := s.KV.Get(ctx, mediaapp.MediaKey(tenantID, mediaID), &row); err != nil {
		return nil, err
	}
	return row, nil
}

func (s *Service) fetchAssetRow(ctx context.Context, tenantID, mediaID, assetID string) (map[string]any, error) {
	var row map[string]any
	if err := s.KV.Get(ctx, mediaapp.AssetKey(tenantID, mediaID, assetID), &row); err != nil {
		return nil, err
	}
	return row, nil
}

// relatedKeysFor builds the "deep-link into /ddb" strings the trace header
// renders. The console splits each entry on "|" to get (pk, sk). The gate
// audit row's SK is a timestamp set at write time so the deep-link points at
// the partition, not a single row — the ddb panel's list-under-PK view
// handles that case.
func relatedKeysFor(jobID string, view *FullJobView) []string {
	keys := []string{"JOB#" + jobID + "|JOB"}
	if view.Summary.MediaID != "" && view.Summary.TenantID != "" {
		keys = append(keys, fmt.Sprintf("%s|%s",
			mediaapp.MediaPK(view.Summary.TenantID, view.Summary.MediaID),
			mediaapp.MediaSK))
		if rid, _ := view.JobAttrs["result_asset_id"].(string); rid != "" {
			keys = append(keys, fmt.Sprintf("%s|%s",
				mediaapp.MediaPK(view.Summary.TenantID, view.Summary.MediaID),
				mediaapp.AssetSK(rid)))
		}
	}
	keys = append(keys, "AUDIT#GATE#"+jobID+"|")
	return keys
}
