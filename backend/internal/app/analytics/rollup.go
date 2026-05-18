// Package analytics — rollup walks ANALYTICS_ACTIVE_TENANTS indices built by
// Sink.Apply and computes per-period top-100 media rankings.
//
// Algorithm per invocation:
//
//  1. For each period, union ANALYTICS_ACTIVE_TENANTS#<day> over the window.
//  2. For each tenant, union CANDIDATE#<tid>#<day> over the same window.
//  3. For each (tenant, media), sum sharded counters VIEW#<tid>#<mediaId>#<N>
//     across the DAY#<yyyymmdd> range implied by the period.
//  4. Rank the top 100 per (tenant, period) and write TOP#<tid>#<period> rows.
//
// Periods: Daily (today), Weekly (7 days), Monthly (30 days),
// Rolling12M (365 days). Rolling12M replaces unbounded all-time so the
// DDB scan never touches rows older than one year.
package analytics

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// Period labels the rollup window stored in TOP rows.
type Period string

const (
	PeriodDaily      Period = "DAILY"
	PeriodWeekly     Period = "WEEKLY"
	PeriodMonthly    Period = "MONTHLY"
	PeriodRolling12M Period = "ROLLING_12M"
)

// AllPeriods is the canonical list evaluated on every rollup run.
var AllPeriods = []Period{PeriodDaily, PeriodWeekly, PeriodMonthly, PeriodRolling12M}

var periodDays = map[Period]int{
	PeriodDaily:      1,
	PeriodWeekly:     7,
	PeriodMonthly:    30,
	PeriodRolling12M: 365,
}

// TopN is the maximum number of entries written per TOP row.
const TopN = 100

// TopEntry is one ranked item stored inside a TOP row.
type TopEntry struct {
	Rank      int    `dynamodbav:"rank"`
	MediaID   string `dynamodbav:"media_id"`
	ViewCount int64  `dynamodbav:"view_count"`
}

// RollupService walks the active-tenant index and writes TOP rows.
type RollupService struct {
	KV  kv.KV
	Now func() time.Time
}

// NewRollupService binds the rollup to a kv driver.
func NewRollupService(k kv.KV) *RollupService {
	return &RollupService{KV: k, Now: func() time.Time { return time.Now().UTC() }}
}

// Run computes top-N rollups for all periods anchored on day. Zero → UTC today.
func (s *RollupService) Run(ctx context.Context, day time.Time) error {
	if day.IsZero() {
		day = s.Now()
	}
	anchor := day.UTC()
	dayStr := anchor.Format("20060102")
	for _, period := range AllPeriods {
		periodDaySet := periodDaysEnding(anchor, period)
		tenants, err := s.queryIndexSuffixUnion(ctx, periodDaySet, func(day string) string {
			return "ANALYTICS_ACTIVE_TENANTS#" + day
		}, "TENANT#")
		if err != nil {
			return fmt.Errorf("rollup: active tenants period=%s anchor=%s: %w", period, dayStr, err)
		}
		for _, tenantID := range tenants {
			candidates, err := s.queryIndexSuffixUnion(ctx, periodDaySet, func(day string) string {
				return "CANDIDATE#" + tenantID + "#" + day
			}, "MEDIA#")
			if err != nil {
				return fmt.Errorf("rollup: candidates tenant=%s period=%s anchor=%s: %w", tenantID, period, dayStr, err)
			}
			if len(candidates) == 0 {
				continue
			}
			counts := make(map[string]int64, len(candidates))
			for _, mediaID := range candidates {
				total, err := s.sumViewShards(ctx, tenantID, mediaID, periodDaySet)
				if err != nil {
					return fmt.Errorf("rollup: sum shards tenant=%s media=%s: %w", tenantID, mediaID, err)
				}
				counts[mediaID] = total
			}
			if err := s.writeTopRow(ctx, tenantID, period, rankTop(counts, TopN)); err != nil {
				return fmt.Errorf("rollup: write top tenant=%s period=%s: %w", tenantID, period, err)
			}
		}
	}
	return nil
}

func periodDaysEnding(anchor time.Time, period Period) []string {
	days := make([]string, periodDays[period])
	for i := range periodDays[period] {
		days[i] = anchor.AddDate(0, 0, -i).Format("20060102")
	}
	return days
}

func (s *RollupService) queryIndexSuffixUnion(ctx context.Context, days []string, pkForDay func(string) string, skPrefix string) ([]string, error) {
	seen := make(map[string]struct{})
	for _, day := range days {
		values, err := s.queryIndexSuffix(ctx, pkForDay(day), skPrefix)
		if err != nil {
			return nil, err
		}
		for _, value := range values {
			seen[value] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for value := range seen {
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}

// queryIndexSuffix walks all items under pk and returns the suffix after prefix
// from each row's SK. Used for the active-tenant and candidate indexes.
func (s *RollupService) queryIndexSuffix(ctx context.Context, pk, prefix string) ([]string, error) {
	var out []string
	var startKey *kv.Key
	for {
		page, err := s.KV.Query(ctx, kv.QueryRequest{
			KeyConditionExpression: "PK = :pk",
			ExpressionAttributeValues: kv.Values{
				":pk": pk,
			},
			ExclusiveStartKey: startKey,
		})
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			sk, _ := item.Get("SK").(string)
			if len(sk) > len(prefix) && sk[:len(prefix)] == prefix {
				out = append(out, sk[len(prefix):])
			}
		}
		if page.LastEvaluatedKey == nil {
			break
		}
		startKey = page.LastEvaluatedKey
	}
	return out, nil
}

func (s *RollupService) sumViewShards(ctx context.Context, tenantID, mediaID string, days []string) (int64, error) {
	return sumCounterShards(ctx, s.KV, "VIEW", tenantID, mediaID, days)
}

func rankTop(counts map[string]int64, n int) []TopEntry {
	type pair struct {
		mediaID string
		count   int64
	}
	pairs := make([]pair, 0, len(counts))
	for k, v := range counts {
		pairs = append(pairs, pair{k, v})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].mediaID < pairs[j].mediaID
	})
	if len(pairs) > n {
		pairs = pairs[:n]
	}
	entries := make([]TopEntry, len(pairs))
	for i, p := range pairs {
		entries[i] = TopEntry{Rank: i + 1, MediaID: p.mediaID, ViewCount: p.count}
	}
	return entries
}

func (s *RollupService) writeTopRow(ctx context.Context, tenantID string, period Period, entries []TopEntry) error {
	return s.KV.Put(ctx, map[string]any{
		"PK":           TopPK(tenantID, string(period)),
		"SK":           TopSK,
		"entries":      entries,
		"generated_at": s.Now().Format(time.RFC3339),
	}, kv.PutOptions{})
}
