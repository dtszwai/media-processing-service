package media

import (
	"context"
	"errors"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency/persist"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// EnqueueDerive stakes an IDEMPOTENCY#asset:<scope> claim and stages a
// media.v1.process outbox row in a single TransactWriteItems. On collision
// (the claim row already exists) it reads back the previous claim and
// returns the cached result string with replay=true. A second call with
// the same idempotency_key but a different input hash returns
// ErrIdempotencyKeyReused — overwriting a prior result would silently
// change the asset ids the caller cached.
func (r *DDBRepo) EnqueueDerive(ctx context.Context, in DeriveEnqueueInput) (string, bool, error) {
	claim := persist.NewCompletedClaim(in.Scope, in.InputHash, in.Result, in.Now, in.ClaimTTL)
	outboxOp := outbox.BuildPutOp(in.Row)
	err := r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{Item: claim, ConditionExpression: "attribute_not_exists(PK)"}},
		{Put: &outboxOp},
	})
	if err == nil {
		return in.Result, false, nil
	}
	if !errors.Is(err, kv.ErrConditionFailed) {
		return "", false, err
	}
	cachedResult, cachedHash, _, gerr := persist.GetResultWithHash(ctx, r.KV, in.Scope)
	if gerr != nil {
		return "", false, errors.Join(kv.ErrConditionFailed, gerr)
	}
	if cachedHash != in.InputHash {
		return "", false, ErrIdempotencyKeyReused
	}
	return cachedResult, true, nil
}
