package ddb

import (
	"context"
	"errors"
	"fmt"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

// ResourceLessor is the production ResourceLessor. It writes a LEASE# row
// per concurrent provider slot keyed by (provider, resource class, optional
// tenant) so multiple workers can negotiate capacity without an external
// lock service.
//
// Row shape (single-table convention):
//
//	PK = LEASE#provider:<provider>#resource:<class>[#tenant:<tenant>]
//	SK = HOLD
//	held_by         (workflow instance / job id)
//	granted_at      (RFC3339Nano)
//	expires_at      (RFC3339Nano)
//	ttl_epoch (int64, for the gsi_lease_expiry range key — lex-sortable)
//	renewed_at      (RFC3339Nano)
//	gsi_lease_pk    = "LEASE_HOLD"   (constant, lets the sweep scan the GSI)
//	gsi_lease_sk    = ttl_epoch as a zero-padded decimal string
//
// All mutations are conditional: Acquire either creates a new row OR steals
// an expired one (ttl_epoch < :now). Renew and Release require
// held_by = :me so a crashed-then-reclaimed holder cannot corrupt the row.
type ResourceLessor struct {
	KV  kv.KV
	Now func() time.Time
	// InstanceID is the "held_by" identifier this lessor stamps on rows it
	// acquires. Defaults to a per-process random id; tests override it to
	// deterministically simulate two distinct holders.
	InstanceID string
	// SweepBatch is the max number of expired rows the sweep reads per Query
	// page. Sweep continues paging until LastEvaluatedKey is nil.
	SweepBatch int32
}

// NewResourceLessor wires the lessor to a KV driver. Use this from bootstrap;
// production wires KV = DDBKV(media table), tests wire a fake KV.
func NewResourceLessor(k kv.KV) *ResourceLessor {
	return &ResourceLessor{
		KV:         k,
		Now:        func() time.Time { return time.Now().UTC() },
		InstanceID: "lessor-" + randid.New(),
		SweepBatch: 64,
	}
}

const (
	// LeaseSKHold is the constant SK for a held-lease row. One PK ↔ one row.
	LeaseSKHold = "HOLD"
	// LeaseGSIPartition is the constant gsi_lease_pk on every lease row.
	// The sweep queries this partition and walks ttl_epoch.
	LeaseGSIPartition = "LEASE_HOLD"
)

// leasePK builds the canonical lease partition key. tenant is optional and
// distinguishes per-tenant capacity caps (e.g. an enterprise tenant with a
// dedicated provider quota) from cross-tenant shared capacity.
func leasePK(class generation.ResourceClass, tenant string) string {
	base := "LEASE#resource:" + string(class)
	if tenant != "" {
		base += "#tenant:" + tenant
	}
	return base
}

// gsiSK is the gsi_lease_sk value: zero-padded decimal unix seconds. The
// sweep's KeyConditionExpression compares against this lexicographically.
func gsiSK(unix int64) string {
	return fmt.Sprintf("%020d", unix)
}

// AcquireResource stakes a fresh lease (or steals an expired one). On
// success returns the lease with the held_by token (lease.ID). On capacity
// unavailable returns a transient-classified error so SQS retries via
// visibility-timeout backoff.
//
// The condition `attribute_not_exists(PK) OR ttl_epoch < :now` is
// what guarantees mutual exclusion: only one writer's UpdateItem succeeds
// when two workers race for the same key.
func (l *ResourceLessor) AcquireResource(ctx context.Context, req genapp.LeaseRequest) (*genapp.ResourceLease, error) {
	if req.ResourceClass == "" {
		return nil, errors.New("lease: ResourceClass required")
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	now := l.now()
	expires := now.Add(ttl)
	pk := leasePK(req.ResourceClass, req.TenantID)
	leaseID := l.InstanceID + ":" + randid.New()

	err := l.KV.Update(ctx, kv.UpdateOp{
		Key:                 kv.Key{PK: pk, SK: LeaseSKHold},
		ConditionExpression: "attribute_not_exists(PK) OR ttl_epoch < :now_unix",
		UpdateExpression:    "SET held_by = :me, granted_at = :now, expires_at = :exp, ttl_epoch = :exp_unix, renewed_at = :now, gsi_lease_pk = :gsi_pk, gsi_lease_sk = :gsi_sk, tenant_id = :tenant, job_id = :job, resource_class = :rc",
		ExpressionAttributeValues: kv.Values{
			":me":       leaseID,
			":now":      now.Format(time.RFC3339Nano),
			":exp":      expires.Format(time.RFC3339Nano),
			":exp_unix": expires.Unix(),
			":now_unix": now.Unix(),
			":gsi_pk":   LeaseGSIPartition,
			":gsi_sk":   gsiSK(expires.Unix()),
			":tenant":   req.TenantID,
			":job":      req.JobID,
			":rc":       string(req.ResourceClass),
		},
	})
	if err != nil {
		if errors.Is(err, kv.ErrConditionFailed) {
			return nil, generation.Transient("RESOURCE_CAPACITY_UNAVAILABLE",
				"another holder owns the lease and it has not yet expired")
		}
		return nil, fmt.Errorf("lease acquire: %w", err)
	}
	return &genapp.ResourceLease{
		ID:            leaseID,
		ResourceClass: req.ResourceClass,
		TenantID:      req.TenantID,
		JobID:         req.JobID,
		ExpiresAt:     expires,
	}, nil
}

// RenewResource pushes the expiry forward conditional on held_by == leaseID.
// Use this when a provider call runs longer than the original TTL so the
// reaper doesn't sweep an active holder.
func (l *ResourceLessor) RenewResource(ctx context.Context, leaseID string, class generation.ResourceClass, tenant string, ttl time.Duration) error {
	if leaseID == "" || class == "" {
		return errors.New("lease: leaseID + ResourceClass required")
	}
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	now := l.now()
	expires := now.Add(ttl)
	pk := leasePK(class, tenant)
	err := l.KV.Update(ctx, kv.UpdateOp{
		Key:                 kv.Key{PK: pk, SK: LeaseSKHold},
		ConditionExpression: "held_by = :me",
		UpdateExpression:    "SET expires_at = :exp, ttl_epoch = :exp_unix, renewed_at = :now, gsi_lease_sk = :gsi_sk",
		ExpressionAttributeValues: kv.Values{
			":me":       leaseID,
			":now":      now.Format(time.RFC3339Nano),
			":exp":      expires.Format(time.RFC3339Nano),
			":exp_unix": expires.Unix(),
			":gsi_sk":   gsiSK(expires.Unix()),
		},
	})
	if err != nil {
		if errors.Is(err, kv.ErrConditionFailed) {
			return generation.Terminal("LEASE_LOST", "renew condition failed — another worker reclaimed or the lease expired")
		}
		return fmt.Errorf("lease renew: %w", err)
	}
	return nil
}

// ReleaseResource conditionally deletes the lease row. Misuse — wrong
// holder, already-swept row — collapses to nil so a stale Release after
// reclaim cannot disturb the new holder. The sweep is the long-stop GC.
func (l *ResourceLessor) ReleaseResource(ctx context.Context, lease *genapp.ResourceLease) error {
	if lease == nil || lease.ID == "" || lease.ResourceClass == "" {
		return nil
	}
	pk := leasePK(lease.ResourceClass, lease.TenantID)
	err := l.KV.Delete(ctx, kv.DeleteOp{
		Key:                 kv.Key{PK: pk, SK: LeaseSKHold},
		ConditionExpression: "held_by = :me",
		ExpressionAttributeValues: kv.Values{
			":me": lease.ID,
		},
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, kv.ErrConditionFailed) {
		// Already reclaimed/expired by another worker — nothing to release.
		return nil
	}
	return fmt.Errorf("lease release: %w", err)
}

// SweepResult is what SweepExpired reports back so callers can record a
// metric per invocation.
type SweepResult struct {
	Scanned int64
	Deleted int64
}

// SweepExpired queries gsi_lease_expiry for rows whose ttl_epoch is
// strictly less than the current time and conditionally deletes each. The
// `ttl_epoch < :seen` guard on the DELETE ensures we don't race with
// a concurrent Renew that just pushed the same row's expiry forward.
//
// Returns the count of rows scanned vs deleted. The two numbers may differ
// when a Renew won the race against the sweep — that's correct behavior, not
// a bug.
func (l *ResourceLessor) SweepExpired(ctx context.Context) (SweepResult, error) {
	var (
		scanned int64
		deleted int64
		start   *kv.Key
	)
	now := l.now().Unix()
	batch := l.SweepBatch
	if batch <= 0 {
		batch = 64
	}
	for {
		page, err := l.KV.Query(ctx, kv.QueryRequest{
			Index:                  "gsi_lease_expiry",
			KeyConditionExpression: "gsi_lease_pk = :pk AND gsi_lease_sk < :now",
			ExpressionAttributeValues: kv.Values{
				":pk":  LeaseGSIPartition,
				":now": gsiSK(now),
			},
			Limit:             batch,
			ExclusiveStartKey: start,
		})
		if err != nil {
			return SweepResult{Scanned: scanned, Deleted: deleted}, fmt.Errorf("lease sweep query: %w", err)
		}
		for _, row := range page.Items {
			scanned++
			var r struct {
				PK       string `dynamodbav:"PK"`
				SK       string `dynamodbav:"SK"`
				TTLEpoch int64  `dynamodbav:"ttl_epoch"`
			}
			if err := row.Unmarshal(&r); err != nil {
				continue
			}
			if r.PK == "" || r.SK == "" {
				continue
			}
			// Conditional delete: only remove the row if it is still expired
			// against this exact seen-expiry. If another worker Renewed
			// between Query and Delete, the condition fails and we skip it.
			derr := l.KV.Delete(ctx, kv.DeleteOp{
				Key:                 kv.Key{PK: r.PK, SK: r.SK},
				ConditionExpression: "ttl_epoch = :seen",
				ExpressionAttributeValues: kv.Values{
					":seen": r.TTLEpoch,
				},
			})
			if derr == nil {
				deleted++
				continue
			}
			if errors.Is(derr, kv.ErrConditionFailed) {
				// Renew won the race; leave the row alone.
				continue
			}
			return SweepResult{Scanned: scanned, Deleted: deleted}, fmt.Errorf("lease sweep delete: %w", derr)
		}
		if page.LastEvaluatedKey == nil {
			break
		}
		start = page.LastEvaluatedKey
	}
	return SweepResult{Scanned: scanned, Deleted: deleted}, nil
}

func (l *ResourceLessor) now() time.Time {
	if l.Now != nil {
		return l.Now()
	}
	return time.Now().UTC()
}
