package ddb

import (
	"context"
	"errors"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency/persist"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

// Idempotency implements the idempotency.Store port on the single table.
//
// Schema (one row per claim scope):
//
//	PK = IDEMPOTENCY#<scope>, SK = CLAIM
//	input_hash, status (CLAIMED|COMPLETED|FAILED), result, error_code,
//	claim_token (UUID), lease_until (unix), attempts, ttl_epoch, created_at,
//	updated_at
//
// Complete/Fail are conditional on claim_token = token so a crashed worker's
// stale lease cannot terminally write after another worker reclaimed.
type Idempotency struct {
	KV  kv.KV
	Now func() time.Time
}

// NewIdempotency binds the impl to a kv driver.
func NewIdempotency(k kv.KV) *Idempotency {
	return &Idempotency{KV: k, Now: func() time.Time { return time.Now().UTC() }}
}

type idemRow struct {
	PK         string `dynamodbav:"PK"`
	SK         string `dynamodbav:"SK"`
	InputHash  string `dynamodbav:"input_hash"`
	Status     string `dynamodbav:"status"`
	Result     string `dynamodbav:"result,omitempty"`
	ErrorCode  string `dynamodbav:"error_code,omitempty"`
	ClaimToken string `dynamodbav:"claim_token"`
	LeaseUntil int64  `dynamodbav:"lease_until"`
	Attempts   int    `dynamodbav:"attempts"`
	TTLEpoch   int64  `dynamodbav:"ttl_epoch"`
	CreatedAt  string `dynamodbav:"created_at"`
	UpdatedAt  string `dynamodbav:"updated_at"`
}

const idemTTL = 14 * 24 * time.Hour

func (s *Idempotency) Claim(ctx context.Context, scope, inputHash string, lease time.Duration) (idempotency.Outcome, string, error) {
	if scope == "" || inputHash == "" {
		return "", "", errors.New("idempotency: scope + input_hash required")
	}
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	now := s.Now()
	token := randid.New()
	row := idemRow{
		PK:         persist.PK(scope),
		SK:         persist.ClaimSK,
		InputHash:  inputHash,
		Status:     string(idempotency.StatusClaimed),
		ClaimToken: token,
		LeaseUntil: now.Add(lease).Unix(),
		Attempts:   1,
		TTLEpoch:   now.Add(idemTTL).Unix(),
		CreatedAt:  now.Format(time.RFC3339Nano),
		UpdatedAt:  now.Format(time.RFC3339Nano),
	}
	err := s.KV.Put(ctx, row, kv.PutOptions{
		ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
	})
	if err == nil {
		return idempotency.OutcomeNew, token, nil
	}
	if !errors.Is(err, kv.ErrConditionFailed) {
		return "", "", err
	}
	var existing idemRow
	if gerr := s.KV.Get(ctx, persist.Key(scope), &existing); gerr != nil {
		return "", "", gerr
	}
	if existing.InputHash != inputHash {
		return idempotency.OutcomeConflict, "", nil
	}
	switch idempotency.Status(existing.Status) {
	case idempotency.StatusCompleted:
		return idempotency.OutcomeReplayCompleted, "", nil
	case idempotency.StatusFailed:
		return idempotency.OutcomeReplayFailed, "", nil
	}
	if existing.LeaseUntil > now.Unix() {
		return idempotency.OutcomeReplayClaimedFresh, "", nil
	}
	return idempotency.OutcomeReplayClaimedStale, "", nil
}

func (s *Idempotency) Complete(ctx context.Context, scope, token, ref string) error {
	return s.KV.Update(ctx, kv.UpdateOp{
		Key:                 persist.Key(scope),
		ConditionExpression: "claim_token = :t AND #s = :claimed",
		UpdateExpression:    "SET #s = :s, #r = :r, updated_at = :u",
		ExpressionAttributeNames: kv.Names{
			"#s": "status",
			"#r": "result",
		},
		ExpressionAttributeValues: kv.Values{
			":t":       token,
			":claimed": string(idempotency.StatusClaimed),
			":s":       string(idempotency.StatusCompleted),
			":r":       ref,
			":u":       s.Now().Format(time.RFC3339Nano),
		},
	})
}

func (s *Idempotency) Fail(ctx context.Context, scope, token, code string) error {
	return s.KV.Update(ctx, kv.UpdateOp{
		Key:                 persist.Key(scope),
		ConditionExpression: "claim_token = :t AND #s = :claimed",
		UpdateExpression:    "SET #s = :s, error_code = :e, updated_at = :u",
		ExpressionAttributeNames: kv.Names{
			"#s": "status",
		},
		ExpressionAttributeValues: kv.Values{
			":t":       token,
			":claimed": string(idempotency.StatusClaimed),
			":s":       string(idempotency.StatusFailed),
			":e":       code,
			":u":       s.Now().Format(time.RFC3339Nano),
		},
	})
}

func (s *Idempotency) GetResult(ctx context.Context, scope string) (string, idempotency.Status, error) {
	var row idemRow
	if err := s.KV.Get(ctx, persist.Key(scope), &row); err != nil {
		return "", "", err
	}
	return row.Result, idempotency.Status(row.Status), nil
}

// GetResultWithHash returns the result plus persisted input_hash so submit
// replay paths can validate the caller's input against what was originally
// claimed. Same scope + different hash means the caller reused an
// idempotency_key with a different request shape — almost always a bug.
func (s *Idempotency) GetResultWithHash(ctx context.Context, scope string) (string, string, idempotency.Status, error) {
	return persist.GetResultWithHash(ctx, s.KV, scope)
}

func (s *Idempotency) Reclaim(ctx context.Context, scope string, lease time.Duration) (string, error) {
	if lease <= 0 {
		lease = 5 * time.Minute
	}
	now := s.Now()
	newToken := randid.New()
	err := s.KV.Update(ctx, kv.UpdateOp{
		Key:                 persist.Key(scope),
		ConditionExpression: "attribute_exists(claim_token) AND #s = :claimed AND lease_until <= :now",
		UpdateExpression:    "SET claim_token = :nt, lease_until = :lu, attempts = attempts + :one, updated_at = :u",
		ExpressionAttributeNames: kv.Names{
			"#s": "status",
		},
		ExpressionAttributeValues: kv.Values{
			":nt":      newToken,
			":lu":      now.Add(lease).Unix(),
			":now":     now.Unix(),
			":one":     1,
			":u":       now.Format(time.RFC3339Nano),
			":claimed": string(idempotency.StatusClaimed),
		},
	})
	if err != nil {
		return "", err
	}
	return newToken, nil
}

func (s *Idempotency) Abandon(ctx context.Context, scope, token string) error {
	return s.KV.Delete(ctx, kv.DeleteOp{
		Key:                 persist.Key(scope),
		ConditionExpression: "claim_token = :t",
		ExpressionAttributeValues: kv.Values{
			":t": token,
		},
	})
}
