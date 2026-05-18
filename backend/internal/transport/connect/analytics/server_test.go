package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	connect "connectrpc.com/connect"

	analyticsapp "github.com/dtszwai/media-processing-service/backend/internal/app/analytics"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
	analyticspb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/analytics/v1"
)

type stubReader struct {
	topEntries     map[string][]analyticsapp.TopEntry
	viewCount      int64
	downloadTotal  int64
	downloadFormat map[string]int64
	err            error
}

func (s *stubReader) GetTopEntries(_ context.Context, _ string, period analyticsapp.Period) ([]analyticsapp.TopEntry, error) {
	if s.err != nil {
		return nil, s.err
	}
	return s.topEntries[string(period)], nil
}

func (s *stubReader) SumViewCountRange(_ context.Context, _, _ string, _ []string) (int64, error) {
	return s.viewCount, s.err
}

func (s *stubReader) SumDownloadCountRange(_ context.Context, _, _ string, _ []string) (int64, error) {
	return s.downloadTotal, s.err
}

func (s *stubReader) SumDownloadsByFormat(_ context.Context, _ string, _ []string, _ []string) (int64, map[string]int64, error) {
	return s.downloadTotal, s.downloadFormat, s.err
}

func TestPeriodFromProto(t *testing.T) {
	cases := []struct {
		in   string
		want analyticsapp.Period
	}{
		{"DAILY", analyticsapp.PeriodDaily},
		{"", analyticsapp.PeriodDaily},
		{"TODAY", analyticsapp.PeriodDaily},
		{"THIS_WEEK", analyticsapp.PeriodWeekly},
		{"WEEKLY", analyticsapp.PeriodWeekly},
		{"THIS_MONTH", analyticsapp.PeriodMonthly},
		{"MONTHLY", analyticsapp.PeriodMonthly},
		{"THIS_YEAR", analyticsapp.PeriodRolling12M},
		{"ROLLING_12M", analyticsapp.PeriodRolling12M},
	}
	for _, c := range cases {
		if got := periodFromProto(c.in); got != c.want {
			t.Errorf("periodFromProto(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}

func TestDaysBack(t *testing.T) {
	anchor := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	days := daysBack(anchor, 3)
	if len(days) != 3 {
		t.Fatalf("len = %d, want 3", len(days))
	}
	if days[0] != "20260101" {
		t.Errorf("days[0] = %s, want 20260101", days[0])
	}
	if days[2] != "20251230" {
		t.Errorf("days[2] = %s, want 20251230", days[2])
	}
}

func TestToProtoItems_CapsAtLimit(t *testing.T) {
	entries := []analyticsapp.TopEntry{{Rank: 1, MediaID: "m-1", ViewCount: 100}, {Rank: 2, MediaID: "m-2", ViewCount: 50}}
	items := toProtoItems(entries, 1)
	if len(items) != 1 {
		t.Fatalf("len = %d, want 1", len(items))
	}
	if items[0].EntityId != "m-1" {
		t.Errorf("item[0].entity_id = %s, want m-1", items[0].EntityId)
	}
}

func TestGetTopMedia_ReturnsUnauthenticatedWithoutPrincipal(t *testing.T) {
	srv := NewServer(&stubReader{})
	_, err := srv.GetTopMedia(context.Background(), connect.NewRequest(&analyticspb.GetTopMediaRequest{}))
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Errorf("expected Unauthenticated, got %v", err)
	}
}

func TestGetMediaViews_ReturnsInvalidArgumentWithoutMediaID(t *testing.T) {
	srv := NewServer(&stubReader{})
	ctx := jwtauth.WithPrincipal(context.Background(), jwtauth.Principal{TenantID: "tenant-1", UserID: "user-1"})
	_, err := srv.GetMediaViews(ctx, connect.NewRequest(&analyticspb.GetMediaViewsRequest{}))
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("expected InvalidArgument, got %v", err)
	}
}

func TestGetTopMedia_ReturnsInternalOnReaderError(t *testing.T) {
	srv := NewServer(&stubReader{err: errors.New("boom")})
	ctx := jwtauth.WithPrincipal(context.Background(), jwtauth.Principal{TenantID: "tenant-1", UserID: "user-1"})
	_, err := srv.GetTopMedia(ctx, connect.NewRequest(&analyticspb.GetTopMediaRequest{}))
	if connect.CodeOf(err) != connect.CodeInternal {
		t.Errorf("expected Internal, got %v", err)
	}
}

func TestDaysForPeriodLabel(t *testing.T) {
	cases := map[string]int{
		"TODAY":       1,
		"THIS_WEEK":   7,
		"THIS_MONTH":  30,
		"THIS_YEAR":   365,
		"ROLLING_12M": 365,
	}
	for label, want := range cases {
		got := daysForPeriodLabel(label)
		if len(got) != want {
			t.Errorf("daysForPeriodLabel(%s) len = %d, want %d", label, len(got), want)
		}
	}
}
