package quota

import (
	"context"
	"errors"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

type ledgerRow struct {
	Amount int64  `dynamodbav:"amount"`
	State  string `dynamodbav:"state"`
}

func (r *Repo) ledgerReplayMatches(ctx context.Context, scope quota.ScopeType, scopeID string, metric quota.Metric, period, reservationID string, amount int64, allowed ...quota.ReservationState) (bool, error) {
	var row ledgerRow
	if err := r.KV.Get(ctx, LedgerKey(scope, scopeID, metric, period, reservationID), &row); err != nil {
		if errors.Is(err, kv.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	if row.Amount != amount {
		return false, nil
	}
	for _, state := range allowed {
		if row.State == string(state) {
			return true, nil
		}
	}
	return false, nil
}

func (r *Repo) ledgerPutOp(scope quota.ScopeType, scopeID string, metric quota.Metric, period string, res quota.Reservation, now time.Time) kv.WriteOp {
	ttl := now.Add(365 * 24 * time.Hour).Unix()
	item := map[string]any{
		"PK":              ReservoirPK(scope, scopeID, metric, period),
		"SK":              LedgerSK(res.ID),
		"reservation_id":  res.ID,
		"scope_type":      string(scope),
		"scope_id":        scopeID,
		"metric":          string(metric),
		"period":          period,
		"amount":          res.Amount,
		"state":           string(quota.ReservationReserved),
		"reason":          res.Reason,
		"pricing_version": res.PricingVersion,
		"job_id":          res.JobID,
		"media_id":        res.MediaID,
		"api_key_id":      res.APIKeyID,
		"reserved_at":     now.Format(time.RFC3339Nano),
		"ttl_epoch":       ttl,
	}
	return kv.WriteOp{Put: &kv.PutOp{
		Item: item,
		// attribute_not_exists(PK) AND attribute_not_exists(SK) so a duplicate
		// reservation ID cancels the transaction — re-issuing the same
		// reservation must not double-decrement the aggregate row.
		ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
	}}
}

func (r *Repo) ledgerCommitOp(scope quota.ScopeType, scopeID string, metric quota.Metric, period, reservationID string, now time.Time) kv.WriteOp {
	return kv.WriteOp{Update: &kv.UpdateOp{
		Key:                 LedgerKey(scope, scopeID, metric, period, reservationID),
		UpdateExpression:    "SET #state = :committed, committed_at = :now",
		ConditionExpression: "#state = :reserved",
		ExpressionAttributeNames: kv.Names{
			"#state": "state",
		},
		ExpressionAttributeValues: kv.Values{
			":reserved":  string(quota.ReservationReserved),
			":committed": string(quota.ReservationCommitted),
			":now":       now.Format(time.RFC3339Nano),
		},
	}}
}

func (r *Repo) ledgerReleaseOp(scope quota.ScopeType, scopeID string, metric quota.Metric, period, reservationID string, now time.Time) kv.WriteOp {
	return kv.WriteOp{Update: &kv.UpdateOp{
		Key:                 LedgerKey(scope, scopeID, metric, period, reservationID),
		UpdateExpression:    "SET #state = :released, released_at = :now",
		ConditionExpression: "#state = :reserved",
		ExpressionAttributeNames: kv.Names{
			"#state": "state",
		},
		ExpressionAttributeValues: kv.Values{
			":reserved": string(quota.ReservationReserved),
			":released": string(quota.ReservationReleased),
			":now":      now.Format(time.RFC3339Nano),
		},
	}}
}
