package media

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// listMaxLoops caps the filter-under-fill retry loop in ListByTenant. DynamoDB
// applies FilterExpression after the page Limit, so a sparse filter can return
// fewer items than requested. We loop until the page is full or there are no
// more rows, but bound the loops to prevent a runaway query when a tenant's
// entire dataset fails the filter.
const listMaxLoops = 10

func (r *DDBRepo) PutMedia(ctx context.Context, m media.Media) error {
	if m.ID == "" || m.TenantID == "" {
		return fmt.Errorf("%w: media id and tenant_id required", ErrInvalidInput)
	}
	if m.CreatedAt.IsZero() {
		m.CreatedAt = time.Now().UTC()
	}
	m.UpdatedAt = time.Now().UTC()
	return r.KV.Put(ctx, newMediaRow(m), kv.PutOptions{})
}

func (r *DDBRepo) GetMedia(ctx context.Context, tenantID, mediaID string) (*media.Media, error) {
	var row mediaRow
	if err := r.KV.Get(ctx, MediaKey(tenantID, mediaID), &row); err != nil {
		return nil, err
	}
	out := row.toDomain()
	return &out, nil
}

// CompleteMediaIfProcessing promotes RUNNING → COMPLETE. Already-COMPLETE
// or DELETED rows are no-ops (do not resurrect a soft-delete).
func (r *DDBRepo) CompleteMediaIfProcessing(ctx context.Context, tenantID, mediaID string, now time.Time) error {
	err := r.KV.Update(ctx, kv.UpdateOp{
		Key:                 MediaKey(tenantID, mediaID),
		ConditionExpression: "lifecycle = :processing",
		UpdateExpression:    "SET lifecycle = :complete, updated_at = :now, gsi_lifecycle_pk = :lc_pk, gsi_lifecycle_sk = :now",
		ExpressionAttributeValues: kv.Values{
			":processing": string(media.LifecycleRunning),
			":complete":   string(media.LifecycleComplete),
			":now":        now.Format(time.RFC3339Nano),
			":lc_pk":      LifecycleGSIPK(tenantID, string(media.LifecycleComplete)),
		},
	})
	if err == nil {
		return nil
	}
	if !errors.Is(err, kv.ErrConditionFailed) {
		return err
	}
	m, gerr := r.GetMedia(ctx, tenantID, mediaID)
	if gerr != nil {
		return errors.Join(kv.ErrConditionFailed, gerr)
	}
	if m.Lifecycle == media.LifecycleComplete || m.Lifecycle == media.LifecycleDeleted {
		return nil
	}
	return fmt.Errorf("%w: cannot complete from current lifecycle", ErrPreconditionFailed)
}

// SoftDeleteMediaAndEnqueue runs the soft-delete + outbox transaction.
func (r *DDBRepo) SoftDeleteMediaAndEnqueue(ctx context.Context, tenantID, mediaID string, retention time.Duration, row outbox.Row, now time.Time) error {
	outboxOp := outbox.BuildPutOp(row)
	err := r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Update: &kv.UpdateOp{
			Key:                 MediaKey(tenantID, mediaID),
			ConditionExpression: "lifecycle IN (:processing, :complete)",
			UpdateExpression:    "SET lifecycle = :deleted, deleted_at = :now, expires_at = :exp, updated_at = :now, gsi_lifecycle_pk = :lc_pk, gsi_lifecycle_sk = :now",
			ExpressionAttributeValues: kv.Values{
				":processing": string(media.LifecycleRunning),
				":complete":   string(media.LifecycleComplete),
				":deleted":    string(media.LifecycleDeleted),
				":now":        now.Format(time.RFC3339Nano),
				":exp":        now.Add(retention).Unix(),
				":lc_pk":      LifecycleGSIPK(tenantID, string(media.LifecycleDeleted)),
			},
		}},
		{Put: &outboxOp},
	})
	if errors.Is(err, kv.ErrConditionFailed) {
		m, gerr := r.GetMedia(ctx, tenantID, mediaID)
		if gerr == nil && m.Lifecycle == media.LifecycleDeleted {
			return nil
		}
		return fmt.Errorf("%w: cannot soft-delete from current lifecycle", ErrPreconditionFailed)
	}
	return err
}

// ListByTenant queries gsi_tenant_media (newest-first) and returns up to
// opts.Limit Media rows. DynamoDB applies FilterExpression after the page
// Limit, which can under-fill a page when many rows are filtered out. We loop
// up to listMaxLoops times to fill the page before returning, advancing the
// cursor each iteration.
func (r *DDBRepo) ListByTenant(ctx context.Context, tenantID string, opts ListOpts) (ListPage, error) {
	startKey, err := decodeCursor(opts.Cursor)
	if err != nil {
		return ListPage{}, err
	}

	var filterParts []string
	filterVals := kv.Values{
		":pk": TenantMediaGSIPK(tenantID),
	}

	if !opts.IncludeDeleted {
		filterParts = append(filterParts, "lifecycle <> :deleted")
		filterVals[":deleted"] = string(media.LifecycleDeleted)
	}
	if opts.MediaType != "" {
		filterParts = append(filterParts, "media_type = :media_type")
		filterVals[":media_type"] = opts.MediaType
	}
	if opts.Origin != "" {
		filterParts = append(filterParts, "origin = :origin")
		filterVals[":origin"] = opts.Origin
	}
	filterExpr := strings.Join(filterParts, " AND ")

	scanFwd := false // newest-first: ScanIndexForward = false

	items := make([]media.Media, 0, opts.Limit)
	for loop := 0; loop < listMaxLoops && len(items) < opts.Limit; loop++ {
		req := kv.QueryRequest{
			Index:                     "gsi_tenant_media",
			KeyConditionExpression:    "gsi_tenant_media_pk = :pk",
			ExpressionAttributeValues: filterVals,
			Limit:                     int32(opts.Limit - len(items)),
			ExclusiveStartKey:         startKey,
			ScanIndexForward:          &scanFwd,
		}
		if filterExpr != "" {
			req.FilterExpression = filterExpr
		}

		page, qerr := r.KV.Query(ctx, req)
		if qerr != nil {
			return ListPage{}, qerr
		}

		for _, row := range page.Items {
			var mr mediaRow
			if uerr := row.Unmarshal(&mr); uerr != nil {
				return ListPage{}, uerr
			}
			items = append(items, mr.toDomain())
		}

		if page.LastEvaluatedKey == nil {
			return ListPage{
				Items:      items,
				NextCursor: "",
				HasMore:    false,
			}, nil
		}
		startKey = page.LastEvaluatedKey
	}

	return ListPage{
		Items:      items,
		NextCursor: encodeCursor(startKey),
		HasMore:    true,
	}, nil
}
