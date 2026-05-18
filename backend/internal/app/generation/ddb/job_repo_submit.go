package ddb

import (
	"context"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency/persist"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// Submit writes the full submit transaction: Media + result Asset + Job + idempotency claim (COMPLETED with result = jobID:mediaID) + first-stage outbox row, all atomically.
func (r *JobRepo) Submit(ctx context.Context, in genapp.SubmitInput) error {
	job := in.Job
	if job.CreatedAt.IsZero() {
		job.CreatedAt = time.Now().UTC()
	}
	job.UpdatedAt = job.CreatedAt
	now := job.CreatedAt

	jrow, err := r.row(ctx, job)
	if err != nil {
		return err
	}
	m := in.Media
	if m.CreatedAt.IsZero() {
		m.CreatedAt = now
	}
	m.UpdatedAt = now
	mediaOps, err := mediaapp.SubmissionPutOps(m, in.ResultAsset)
	if err != nil {
		return err
	}
	claim := persist.NewCompletedClaim(in.IdempotencyScope, in.InputHash, job.ID+":"+m.ID, now, 24*time.Hour)

	ops := append(mediaOps,
		kv.WriteOp{Put: &kv.PutOp{Item: jrow, ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)"}},
		kv.WriteOp{Put: &kv.PutOp{Item: initialGenerationItem(job, in.InputHash, now), ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)"}},
		kv.WriteOp{Put: &kv.PutOp{Item: initialOutputItem(job, now), ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)"}},
		kv.WriteOp{Put: &kv.PutOp{Item: claim, ConditionExpression: "attribute_not_exists(PK)"}},
		kv.WriteOp{Put: &kv.PutOp{
			Item: outbox.JobItem(outbox.JobRow{
				JobID:         job.ID,
				TenantID:      job.TenantID,
				TenantLane:    genapp.TenantLane(job.TenantID),
				Tier:          string(job.Tier),
				Stage:         string(generation.StageInputModeration),
				ResourceClass: string(generation.ResourceFast),
				Body:          in.FirstStageBody,
				PartitionTS:   now,
			}),
			ConditionExpression: "attribute_not_exists(PK)",
		}},
	)
	return r.KV.TransactWrite(ctx, ops)
}
