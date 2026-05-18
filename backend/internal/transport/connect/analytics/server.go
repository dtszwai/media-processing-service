// Package analytics adapts app/analytics onto the Connect transport surface.
package analytics

import (
	"context"
	"errors"
	"strings"
	"time"

	connect "connectrpc.com/connect"

	analyticsapp "github.com/dtszwai/media-processing-service/backend/internal/app/analytics"
	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	analyticspb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/analytics/v1"
)

type Server struct {
	reader analyticsapp.Reader
}

func NewServer(reader analyticsapp.Reader) *Server {
	return &Server{reader: reader}
}

func (s *Server) GetTopMedia(ctx context.Context, req *connect.Request[analyticspb.GetTopMediaRequest]) (*connect.Response[analyticspb.GetTopMediaResponse], error) {
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	period := periodFromProto(req.Msg.GetPeriod())
	entries, err := s.reader.GetTopEntries(ctx, claims.TenantID, period)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("get top entries: "+err.Error()))
	}
	limit := int(req.Msg.GetLimit())
	if limit <= 0 || limit > analyticsapp.TopN {
		limit = analyticsapp.TopN
	}
	return connect.NewResponse(&analyticspb.GetTopMediaResponse{Items: toProtoItems(entries, limit)}), nil
}

func (s *Server) GetMediaViews(ctx context.Context, req *connect.Request[analyticspb.GetMediaViewsRequest]) (*connect.Response[analyticspb.GetMediaViewsResponse], error) {
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	if req.Msg.GetMediaId() == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("media_id required"))
	}

	now := time.Now().UTC()
	today := []string{now.Format("20060102")}
	weekDays := daysBack(now, 7)
	monthDays := daysBack(now, 30)
	yearDays := daysBack(now, 365)

	todayCount, err := s.reader.SumViewCountRange(ctx, claims.TenantID, req.Msg.GetMediaId(), today)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("sum views today: "+err.Error()))
	}
	weekCount, err := s.reader.SumViewCountRange(ctx, claims.TenantID, req.Msg.GetMediaId(), weekDays)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("sum views week: "+err.Error()))
	}
	monthCount, err := s.reader.SumViewCountRange(ctx, claims.TenantID, req.Msg.GetMediaId(), monthDays)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("sum views month: "+err.Error()))
	}
	yearCount, err := s.reader.SumViewCountRange(ctx, claims.TenantID, req.Msg.GetMediaId(), yearDays)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("sum views year: "+err.Error()))
	}

	return connect.NewResponse(&analyticspb.GetMediaViewsResponse{Stats: &analyticspb.ViewStats{
		EntityType: "MEDIA",
		EntityId:   req.Msg.GetMediaId(),
		Total:      int32(yearCount),
		Today:      int32(todayCount),
		ThisWeek:   int32(weekCount),
		ThisMonth:  int32(monthCount),
		ThisYear:   int32(yearCount),
	}}), nil
}

func (s *Server) GetFormatUsage(ctx context.Context, req *connect.Request[analyticspb.GetFormatUsageRequest]) (*connect.Response[analyticspb.GetFormatUsageResponse], error) {
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	period := req.Msg.GetPeriod()
	if period == "" {
		period = "TODAY"
	}
	_, byFormat, err := s.reader.SumDownloadsByFormat(ctx, claims.TenantID, nil, daysForPeriodLabel(period))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("format usage: "+err.Error()))
	}
	usage := make(map[string]int32, len(byFormat))
	var total int32
	for f, c := range byFormat {
		usage[f] = int32(c)
		total += int32(c)
	}
	return connect.NewResponse(&analyticspb.GetFormatUsageResponse{Stats: &analyticspb.FormatUsageStats{
		Period: period,
		Usage:  usage,
		Total:  total,
	}}), nil
}

func (s *Server) GetDownloadStats(ctx context.Context, req *connect.Request[analyticspb.GetDownloadStatsRequest]) (*connect.Response[analyticspb.GetDownloadStatsResponse], error) {
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	period := req.Msg.GetPeriod()
	if period == "" {
		period = "TODAY"
	}
	total, byFormat, err := s.reader.SumDownloadsByFormat(ctx, claims.TenantID, nil, daysForPeriodLabel(period))
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("download stats: "+err.Error()))
	}
	byFormatI32 := make(map[string]int32, len(byFormat))
	for f, c := range byFormat {
		byFormatI32[f] = int32(c)
	}
	return connect.NewResponse(&analyticspb.GetDownloadStatsResponse{Stats: &analyticspb.DownloadStats{
		Period:         period,
		TotalDownloads: int32(total),
		ByFormat:       byFormatI32,
		ByDay:          map[string]int32{},
	}}), nil
}

func (s *Server) GetAnalyticsSummary(ctx context.Context, _ *connect.Request[analyticspb.GetAnalyticsSummaryRequest]) (*connect.Response[analyticspb.GetAnalyticsSummaryResponse], error) {
	claims, err := authz.Claims(ctx)
	if err != nil {
		return nil, err
	}
	today := []string{time.Now().UTC().Format("20060102")}
	topToday, err := s.reader.GetTopEntries(ctx, claims.TenantID, analyticsapp.PeriodDaily)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("top today: "+err.Error()))
	}
	topRolling, err := s.reader.GetTopEntries(ctx, claims.TenantID, analyticsapp.PeriodRolling12M)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("top rolling 12m: "+err.Error()))
	}
	dlTotal, byFormat, err := s.reader.SumDownloadsByFormat(ctx, claims.TenantID, nil, today)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, errors.New("dl summary: "+err.Error()))
	}

	var viewsToday int32
	for _, e := range topToday {
		viewsToday += int32(e.ViewCount)
	}
	formatUsage := make(map[string]int32, len(byFormat))
	for f, c := range byFormat {
		formatUsage[f] = int32(c)
	}
	return connect.NewResponse(&analyticspb.GetAnalyticsSummaryResponse{Summary: &analyticspb.AnalyticsSummary{
		ViewsToday:          viewsToday,
		DownloadsToday:      int32(dlTotal),
		TopMediaToday:       toProtoItems(topToday, 10),
		TopMediaRolling_12M: toProtoItems(topRolling, 10),
		FormatUsage:         formatUsage,
	}}), nil
}

func periodFromProto(s string) analyticsapp.Period {
	switch strings.ToUpper(s) {
	case "WEEKLY", "THIS_WEEK":
		return analyticsapp.PeriodWeekly
	case "MONTHLY", "THIS_MONTH":
		return analyticsapp.PeriodMonthly
	case "ROLLING_12M", "THIS_YEAR":
		return analyticsapp.PeriodRolling12M
	default:
		return analyticsapp.PeriodDaily
	}
}

func daysForPeriodLabel(period string) []string {
	n := 1
	switch strings.ToUpper(period) {
	case "THIS_WEEK", "WEEKLY":
		n = 7
	case "THIS_MONTH", "MONTHLY":
		n = 30
	case "THIS_YEAR", "ROLLING_12M":
		n = 365
	}
	return daysBack(time.Now().UTC(), n)
}

func daysBack(now time.Time, n int) []string {
	days := make([]string, n)
	for i := range n {
		days[i] = now.AddDate(0, 0, -i).Format("20060102")
	}
	return days
}

func toProtoItems(entries []analyticsapp.TopEntry, limit int) []*analyticspb.EntityViewCount {
	if len(entries) > limit {
		entries = entries[:limit]
	}
	items := make([]*analyticspb.EntityViewCount, len(entries))
	for i, e := range entries {
		items[i] = &analyticspb.EntityViewCount{
			EntityType: "MEDIA",
			EntityId:   e.MediaID,
			ViewCount:  int32(e.ViewCount),
			Rank:       int32(e.Rank),
		}
	}
	return items
}
