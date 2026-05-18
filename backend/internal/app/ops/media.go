package ops

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// MediaRow is the operator-facing projection of a Media aggregate.
type MediaRow struct {
	MediaID         string
	TenantID        string
	OwnerUserID     string
	Origin          string
	MediaType       string
	Lifecycle       string
	OriginalAssetID string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	DeletedAt       *time.Time
	JobID           string
}

// ListMediaFilter narrows what ListMedia returns.
type ListMediaFilter struct {
	TenantID       string
	MediaType      string
	Origin         string
	Lifecycle      string
	IncludeDeleted bool
	Limit          int32
	Cursor         string
}

// ListMedia surfaces the library tab. Same Scan-then-filter shape as
// ListJobs: the LOCAL_ONLY console is small-data, optimizing for a tight
// list endpoint isn't worth the tenant-scoped Query indirection.
func (s *Service) ListMedia(ctx context.Context, f ListMediaFilter) ([]MediaRow, string, error) {
	if s.DDB == nil {
		return nil, "", fmt.Errorf("ops: ddb client not wired")
	}
	in := &dynamodb.ScanInput{
		TableName:        aws.String(s.Table),
		FilterExpression: aws.String("SK = :sk AND begins_with(PK, :pk)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sk": &types.AttributeValueMemberS{Value: mediaapp.MediaSK},
			":pk": &types.AttributeValueMemberS{Value: "TENANT#"},
		},
	}
	rows, cursor, err := scanUntilLimit(ctx, s.DDB, in, f.Cursor, f.Limit, f.decodeMediaRow)
	if err != nil {
		return nil, "", err
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].CreatedAt.After(rows[j].CreatedAt) })
	s.attachGeneratedJobIDs(ctx, rows)
	return rows, cursor, nil
}

func (f ListMediaFilter) decodeMediaRow(av map[string]types.AttributeValue) (MediaRow, bool) {
	row, ok := decodeMediaRow(av)
	if !ok || !f.matches(row) {
		return MediaRow{}, false
	}
	return row, true
}

func (f ListMediaFilter) matches(row MediaRow) bool {
	return (f.IncludeDeleted || row.Lifecycle != "DELETED") &&
		matchesOptional(f.TenantID, row.TenantID) &&
		matchesOptional(f.MediaType, row.MediaType) &&
		matchesOptional(f.Origin, row.Origin) &&
		matchesOptional(f.Lifecycle, row.Lifecycle)
}

func decodeMediaRow(av map[string]types.AttributeValue) (MediaRow, bool) {
	pk, _ := av["PK"].(*types.AttributeValueMemberS)
	sk, _ := av["SK"].(*types.AttributeValueMemberS)
	if pk == nil || sk == nil || sk.Value != mediaapp.MediaSK {
		return MediaRow{}, false
	}
	parts := strings.Split(pk.Value, "#")
	if len(parts) != 4 || parts[0] != "TENANT" || parts[2] != "MEDIA" {
		return MediaRow{}, false
	}
	row := map[string]any{}
	for k, v := range av {
		row[k] = avToAny(v)
	}
	out := MediaRow{
		MediaID:         stringAttr(row, "id"),
		TenantID:        stringAttr(row, "tenant_id"),
		OwnerUserID:     stringAttr(row, "owner_user_id"),
		Origin:          stringAttr(row, "origin"),
		MediaType:       stringAttr(row, "media_type"),
		Lifecycle:       stringAttr(row, "lifecycle"),
		OriginalAssetID: stringAttr(row, "original_asset_id"),
		CreatedAt:       timeAttr(row, "created_at"),
		UpdatedAt:       timeAttr(row, "updated_at"),
	}
	if t := timeAttr(row, "deleted_at"); !t.IsZero() {
		out.DeletedAt = &t
	}
	return out, true
}

// attachGeneratedJobIDs scans the gen partition for each generated media row
// and picks the latest job id that references it. The library tab uses this
// to deep-link directly to /trace/:jobId for GENERATED rows. Per-row lookup
// failures are surfaced via the logger so the operator at least sees the
// reason a deep-link is missing.
func (s *Service) attachGeneratedJobIDs(ctx context.Context, rows []MediaRow) {
	for i := range rows {
		if rows[i].Origin != "GENERATED" {
			continue
		}
		jobID, err := s.findJobForMedia(ctx, rows[i].TenantID, rows[i].MediaID)
		if err != nil {
			if s.Logger != nil {
				s.Logger.WarnContext(ctx, "ops: lookup job for media failed", "media_id", rows[i].MediaID, "err", err)
			}
			continue
		}
		rows[i].JobID = jobID
	}
}

func (s *Service) findJobForMedia(ctx context.Context, tenantID, mediaID string) (string, error) {
	// No direct media → job lookup index; scan the JOB rows with a tenant
	// + media_id filter. Limit=1 isn't enough — DDB would return whichever
	// matching row landed in the first page, not the latest. The library
	// row deep-links into /trace/:jobId, so picking an arbitrary historical
	// attempt would silently misdirect the operator. Scan all matches and
	// return the newest by created_at.
	out, err := s.DDB.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(s.Table),
		FilterExpression: aws.String("item_type = :t AND media_id = :m AND tenant_id = :tid"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":t":   &types.AttributeValueMemberS{Value: "GEN"},
			":m":   &types.AttributeValueMemberS{Value: mediaID},
			":tid": &types.AttributeValueMemberS{Value: tenantID},
		},
	})
	if err != nil || len(out.Items) == 0 {
		return "", err
	}
	var latestID string
	var latestAt time.Time
	for _, av := range out.Items {
		row := map[string]any{}
		for k, v := range av {
			row[k] = avToAny(v)
		}
		t := timeAttr(row, "created_at")
		if latestID == "" || t.After(latestAt) {
			latestID = stringAttr(row, "id")
			latestAt = t
		}
	}
	return latestID, nil
}

// FullMediaView is what /media/:id returns. assets is every asset row under
// the media partition.
type FullMediaView struct {
	Row    MediaRow
	Media  map[string]any
	Assets []map[string]any
	JobID  string
}

// LocateMedia resolves a media id to its (tenant, mediaID) pair by scanning
// the table for the matching Media row. The console is single-tenant so a
// scan with a tenant_id filter would be redundant; this is the fast path
// for /media/:id when the caller knows only the media id.
func (s *Service) LocateMedia(ctx context.Context, mediaID string) (string, string, bool) {
	if s.DDB == nil || mediaID == "" {
		return "", "", false
	}
	out, err := s.DDB.Scan(ctx, &dynamodb.ScanInput{
		TableName:        aws.String(s.Table),
		FilterExpression: aws.String("SK = :sk AND id = :id"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sk": &types.AttributeValueMemberS{Value: mediaapp.MediaSK},
			":id": &types.AttributeValueMemberS{Value: mediaID},
		},
		Limit: aws.Int32(1),
	})
	if err != nil || len(out.Items) == 0 {
		return "", "", false
	}
	row := map[string]any{}
	for k, v := range out.Items[0] {
		row[k] = avToAny(v)
	}
	tenantID := stringAttr(row, "tenant_id")
	id := stringAttr(row, "id")
	if tenantID == "" || id == "" {
		return "", "", false
	}
	return tenantID, id, true
}

func (s *Service) GetMedia(ctx context.Context, tenantID, mediaID string) (*FullMediaView, error) {
	if tenantID == "" || mediaID == "" {
		return nil, fmt.Errorf("ops: tenant_id + media_id required")
	}
	rows, err := s.queryAll(ctx, kv.QueryRequest{
		KeyConditionExpression:    "PK = :pk",
		ExpressionAttributeValues: kv.Values{":pk": mediaapp.MediaPK(tenantID, mediaID)},
		ConsistentRead:            true,
	})
	if err != nil {
		return nil, err
	}
	view := &FullMediaView{}
	for _, row := range rows {
		switch sk := stringAttr(row, "SK"); {
		case sk == mediaapp.MediaSK:
			view.Media = row
			view.Row = MediaRow{
				MediaID:         stringAttr(row, "id"),
				TenantID:        stringAttr(row, "tenant_id"),
				OwnerUserID:     stringAttr(row, "owner_user_id"),
				Origin:          stringAttr(row, "origin"),
				MediaType:       stringAttr(row, "media_type"),
				Lifecycle:       stringAttr(row, "lifecycle"),
				OriginalAssetID: stringAttr(row, "original_asset_id"),
				CreatedAt:       timeAttr(row, "created_at"),
				UpdatedAt:       timeAttr(row, "updated_at"),
			}
			if t := timeAttr(row, "deleted_at"); !t.IsZero() {
				view.Row.DeletedAt = &t
			}
		case strings.HasPrefix(sk, "ASSET#"):
			view.Assets = append(view.Assets, row)
		}
	}
	if view.Row.Origin == "GENERATED" {
		if id, err := s.findJobForMedia(ctx, tenantID, mediaID); err == nil {
			view.JobID = id
		}
	}
	return view, nil
}
