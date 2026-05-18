package analytics

import (
	"context"
	"errors"
	"strconv"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// DDBReader implements Reader against the kv port.
type DDBReader struct {
	KV kv.KV
}

// NewDDBReader binds the reader to a kv driver.
func NewDDBReader(k kv.KV) *DDBReader { return &DDBReader{KV: k} }

// GetTopEntries fetches the pre-computed TOP row for (tenant, period).
func (r *DDBReader) GetTopEntries(ctx context.Context, tenantID string, period Period) ([]TopEntry, error) {
	var row struct {
		Entries []TopEntry `dynamodbav:"entries"`
	}
	if err := r.KV.Get(ctx, kv.Key{PK: TopPK(tenantID, string(period)), SK: TopSK}, &row); err != nil {
		if errors.Is(err, kv.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return row.Entries, nil
}

// SumViewCountRange sums all 16 VIEW shards for (tenant, media) over the days.
func (r *DDBReader) SumViewCountRange(ctx context.Context, tenantID, mediaID string, days []string) (int64, error) {
	return r.sumCounterShards(ctx, "VIEW", tenantID, mediaID, days)
}

// SumDownloadCountRange sums all 16 DOWNLOAD shards for (tenant, media).
func (r *DDBReader) SumDownloadCountRange(ctx context.Context, tenantID, mediaID string, days []string) (int64, error) {
	return r.sumCounterShards(ctx, "DOWNLOAD", tenantID, mediaID, days)
}

// SumDownloadsByFormat returns the total + per-format breakdown by walking
// the DOWNLOAD ledger shards for the tenant across the given days.
func (r *DDBReader) SumDownloadsByFormat(ctx context.Context, tenantID string, mediaIDs []string, days []string) (int64, map[string]int64, error) {
	byFormat := map[string]int64{}
	var total int64
	for _, day := range days {
		for shard := range LedgerShards {
			pk := "DOWNLOAD_EVT#" + day + "#" + strconv.Itoa(shard)
			var startKey *kv.Key
			for {
				page, err := r.KV.Query(ctx, kv.QueryRequest{
					KeyConditionExpression: "PK = :pk",
					FilterExpression:       "tenant_id = :tid",
					ExpressionAttributeValues: kv.Values{
						":pk":  pk,
						":tid": tenantID,
					},
					ExclusiveStartKey: startKey,
				})
				if err != nil {
					return 0, nil, err
				}
				for _, item := range page.Items {
					format, _ := item.Get("format").(string)
					if format == "" {
						format = "unknown"
					}
					byFormat[format]++
					total++
				}
				if page.LastEvaluatedKey == nil {
					break
				}
				startKey = page.LastEvaluatedKey
			}
		}
	}
	return total, byFormat, nil
}

func (r *DDBReader) sumCounterShards(ctx context.Context, kind, tenantID, mediaID string, days []string) (int64, error) {
	return sumCounterShards(ctx, r.KV, kind, tenantID, mediaID, days)
}

// sumCounterShards sums the per-shard `count` attribute across every
// (shard, day) for a given counter kind. The counter PK convention is
// `<kind>#<tenantID>#<mediaID>#<shard>`, SK is `DAY#<yyyymmdd>`. Missing
// rows count as zero. Used by both the read path (rollup TopN computation)
// and the analytics reader (per-media counts returned to API).
func sumCounterShards(ctx context.Context, k kv.KV, kind, tenantID, mediaID string, days []string) (int64, error) {
	var total int64
	base := kind + "#" + tenantID + "#" + mediaID + "#"
	for shard := range ViewShards {
		pk := base + strconv.Itoa(shard)
		for _, day := range days {
			var row struct {
				Count int64 `dynamodbav:"count"`
			}
			err := k.Get(ctx, kv.Key{PK: pk, SK: "DAY#" + day}, &row)
			if err != nil {
				if errors.Is(err, kv.ErrNotFound) {
					continue
				}
				return 0, err
			}
			total += row.Count
		}
	}
	return total, nil
}
