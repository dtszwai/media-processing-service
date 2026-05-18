package ddb

import (
	"context"
	"fmt"
	"strconv"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func (r *JobRepo) LastStageAttempt(ctx context.Context, tenantID, jobID string, stage generation.Stage) (genapp.StageAttempt, error) {
	prefix := "ATTEMPT#" + string(stage) + "#"
	page, err := r.KV.Query(ctx, kv.QueryRequest{
		KeyConditionExpression: "PK = :pk AND begins_with(SK, :prefix)",
		ExpressionAttributeValues: kv.Values{
			":pk":     JobPK(jobID),
			":prefix": prefix,
		},
		ConsistentRead: true,
	})
	if err != nil {
		return genapp.StageAttempt{}, err
	}
	var latest genapp.StageAttempt
	for _, row := range page.Items {
		if tenantID != "" && stringFromRow(row, "tenant_id") != tenantID {
			continue
		}
		attempt := genapp.StageAttempt{
			Stage:        stage,
			StageVersion: uint64(intFromRow(row, "stage_version")),
			AttemptNo:    intFromRow(row, "attempt_no"),
			Result:       stringFromRow(row, "result"),
			ErrorCode:    stringFromRow(row, "error_code"),
			ErrorMessage: stringFromRow(row, "error_message"),
			CreatedAt:    timeFromRow(row, "created_at"),
		}
		if attempt.CreatedAt.IsZero() {
			continue
		}
		if latest.CreatedAt.IsZero() || attempt.CreatedAt.After(latest.CreatedAt) ||
			(attempt.CreatedAt.Equal(latest.CreatedAt) && attempt.AttemptNo > latest.AttemptNo) {
			latest = attempt
		}
	}
	if latest.CreatedAt.IsZero() {
		return genapp.StageAttempt{}, kv.ErrNotFound
	}
	return latest, nil
}

func stringFromRow(row kv.Row, name string) string {
	switch v := row.Get(name).(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

func intFromRow(row kv.Row, name string) int {
	switch v := row.Get(name).(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case string:
		i, _ := strconv.Atoi(v)
		return i
	default:
		return 0
	}
}

func timeFromRow(row kv.Row, name string) time.Time {
	value := stringFromRow(row, name)
	if value == "" || value == "<nil>" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, value)
	return t
}
