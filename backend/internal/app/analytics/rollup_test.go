package analytics

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func TestRankTop_OrdersDescendingAndCapsAtN(t *testing.T) {
	counts := map[string]int64{
		"m-a": 50,
		"m-b": 100,
		"m-c": 75,
		"m-d": 10,
	}
	top := rankTop(counts, 3)
	if len(top) != 3 {
		t.Fatalf("len = %d, want 3", len(top))
	}
	if top[0].MediaID != "m-b" || top[0].ViewCount != 100 || top[0].Rank != 1 {
		t.Errorf("rank 1: got %+v", top[0])
	}
	if top[1].MediaID != "m-c" || top[1].ViewCount != 75 || top[1].Rank != 2 {
		t.Errorf("rank 2: got %+v", top[1])
	}
	if top[2].MediaID != "m-a" || top[2].ViewCount != 50 || top[2].Rank != 3 {
		t.Errorf("rank 3: got %+v", top[2])
	}
}

func TestRankTop_TieBreaksByMediaIDAlpha(t *testing.T) {
	counts := map[string]int64{
		"m-z": 100,
		"m-a": 100,
	}
	top := rankTop(counts, 10)
	if len(top) != 2 {
		t.Fatalf("len = %d, want 2", len(top))
	}
	// alphabetically smaller ID wins on tie
	if top[0].MediaID != "m-a" {
		t.Errorf("tie break: got rank1=%s, want m-a", top[0].MediaID)
	}
}

func TestRankTop_EmptyInput(t *testing.T) {
	top := rankTop(map[string]int64{}, 100)
	if len(top) != 0 {
		t.Fatalf("expected empty, got %d entries", len(top))
	}
}

func TestRankTop_FewerThanN(t *testing.T) {
	counts := map[string]int64{"m-1": 5}
	top := rankTop(counts, 100)
	if len(top) != 1 {
		t.Fatalf("len = %d, want 1", len(top))
	}
}

func TestPeriodDays_AllPeriodsPresent(t *testing.T) {
	for _, p := range AllPeriods {
		if _, ok := periodDays[p]; !ok {
			t.Errorf("period %s has no day count in periodDays", p)
		}
	}
}

type rollupTestRow map[string]any

func (r rollupTestRow) Get(name string) any { return r[name] }
func (r rollupTestRow) Unmarshal(any) error { return nil }

type rollupTestKV struct {
	queryRows map[string][]kv.Row
	counts    map[string]int64
	topRows   map[string][]TopEntry
}

func (k *rollupTestKV) Put(_ context.Context, item kv.Item, _ kv.PutOptions) error {
	raw := item.(map[string]any)
	k.topRows[raw["PK"].(string)] = raw["entries"].([]TopEntry)
	return nil
}

func (k *rollupTestKV) Get(_ context.Context, key kv.Key, out any) error {
	count, ok := k.counts[rollupCountKey(key)]
	if !ok {
		return kv.ErrNotFound
	}
	v := reflect.ValueOf(out)
	if v.Kind() != reflect.Pointer || v.IsNil() {
		return kv.ErrNotFound
	}
	field := v.Elem().FieldByName("Count")
	if !field.IsValid() || !field.CanSet() {
		return kv.ErrNotFound
	}
	field.SetInt(count)
	return nil
}

func (k *rollupTestKV) Query(_ context.Context, req kv.QueryRequest) (kv.QueryResult, error) {
	pk, _ := req.ExpressionAttributeValues[":pk"].(string)
	return kv.QueryResult{Items: k.queryRows[pk]}, nil
}

func (k *rollupTestKV) Update(context.Context, kv.UpdateOp) error { return nil }
func (k *rollupTestKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, nil
}
func (k *rollupTestKV) Delete(context.Context, kv.DeleteOp) error { return nil }
func (k *rollupTestKV) TransactWrite(context.Context, []kv.WriteOp) error {
	return nil
}

func TestRollupRunNonDailyPeriodsUseCandidatesAcrossWholePeriod(t *testing.T) {
	anchor := time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC)
	priorDay := anchor.AddDate(0, 0, -1).Format("20060102")
	k := &rollupTestKV{
		queryRows: map[string][]kv.Row{
			"ANALYTICS_ACTIVE_TENANTS#" + priorDay: {
				rollupTestRow{"SK": "TENANT#tenant-1"},
			},
			"CANDIDATE#tenant-1#" + priorDay: {
				rollupTestRow{"SK": "MEDIA#prior-media"},
			},
		},
		counts: map[string]int64{
			rollupCountKey(kv.Key{PK: "VIEW#tenant-1#prior-media#0", SK: "DAY#" + priorDay}): 7,
		},
		topRows: map[string][]TopEntry{},
	}
	svc := &RollupService{KV: k, Now: func() time.Time { return anchor }}
	if err := svc.Run(context.Background(), anchor); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, ok := k.topRows[TopPK("tenant-1", string(PeriodDaily))]; ok {
		t.Fatal("daily rollup should not include a tenant with no anchor-day activity")
	}
	for _, period := range []Period{PeriodWeekly, PeriodMonthly, PeriodRolling12M} {
		entries := k.topRows[TopPK("tenant-1", string(period))]
		if len(entries) != 1 {
			t.Fatalf("%s entries len = %d, want 1", period, len(entries))
		}
		if entries[0].MediaID != "prior-media" || entries[0].ViewCount != 7 || entries[0].Rank != 1 {
			t.Fatalf("%s entry = %+v, want prior-media count 7 rank 1", period, entries[0])
		}
	}
}

func rollupCountKey(key kv.Key) string {
	return key.PK + "\x00" + key.SK
}
