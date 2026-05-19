package ops

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	quotaapp "github.com/dtszwai/media-processing-service/backend/internal/app/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/quota"
)

type TenantUsageReservoir struct {
	TenantID      string
	Metric        string
	Period        string
	Cap           int64
	Available     int64
	Reserved      int64
	Committed     int64
	Released      int64
	State         string
	PolicyID      string
	PolicyVersion int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Materialized  bool
}

type TenantUsageView struct {
	TenantID           string
	CurrentDailyPeriod string
	DailyCost          *TenantUsageReservoir
	Reservoirs         []TenantUsageReservoir
}

func (s *Service) GetTenantUsage(ctx context.Context, tenantID string) (*TenantUsageView, error) {
	if tenantID == "" {
		tenantID = s.LocalTenantID
	}
	if tenantID == "" {
		return nil, fmt.Errorf("ops: tenant_id required")
	}
	if s.DDB == nil {
		return nil, fmt.Errorf("ops: ddb client not wired")
	}
	rows, err := s.scanTenantReservoirs(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return tenantUsageView(tenantID, quota.PeriodDaily(s.now()), s.TenantCostCapMicroUSD, rows), nil
}

func (s *Service) scanTenantReservoirs(ctx context.Context, tenantID string) ([]TenantUsageReservoir, error) {
	prefix := "RESERVOIR#" + string(quota.ScopeTenant) + "#" + tenantID + "#"
	in := &dynamodb.ScanInput{
		TableName:        aws.String(s.Table),
		ConsistentRead:   aws.Bool(true),
		FilterExpression: aws.String("SK = :sk AND begins_with(PK, :pk)"),
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":sk": &types.AttributeValueMemberS{Value: quotaapp.AggSK},
			":pk": &types.AttributeValueMemberS{Value: prefix},
		},
	}

	var rows []TenantUsageReservoir
	for {
		out, err := s.DDB.Scan(ctx, in)
		if err != nil {
			return nil, fmt.Errorf("ops: scan tenant usage: %w", err)
		}
		for _, av := range out.Items {
			if row, ok := decodeTenantUsageReservoir(av); ok {
				rows = append(rows, row)
			}
		}
		if out.LastEvaluatedKey == nil {
			return rows, nil
		}
		in.ExclusiveStartKey = out.LastEvaluatedKey
	}
}

func tenantUsageView(tenantID, dailyPeriod string, defaultCostCapMicroUSD int64, rows []TenantUsageReservoir) *TenantUsageView {
	sortTenantUsageReservoirs(rows)
	var daily *TenantUsageReservoir
	for i := range rows {
		if rows[i].Metric == string(quota.CostMicroUSD) && rows[i].Period == dailyPeriod {
			daily = &rows[i]
			break
		}
	}
	if daily == nil {
		daily = &TenantUsageReservoir{
			TenantID:     tenantID,
			Metric:       string(quota.CostMicroUSD),
			Period:       dailyPeriod,
			Cap:          defaultCostCapMicroUSD,
			Available:    defaultCostCapMicroUSD,
			State:        string(quota.ReservoirOpen),
			Materialized: false,
		}
	}
	return &TenantUsageView{
		TenantID:           tenantID,
		CurrentDailyPeriod: dailyPeriod,
		DailyCost:          daily,
		Reservoirs:         rows,
	}
}

func decodeTenantUsageReservoir(av map[string]types.AttributeValue) (TenantUsageReservoir, bool) {
	sk, _ := av["SK"].(*types.AttributeValueMemberS)
	if sk == nil || sk.Value != quotaapp.AggSK {
		return TenantUsageReservoir{}, false
	}
	row := map[string]any{}
	for k, v := range av {
		row[k] = avToAny(v)
	}
	if stringAttr(row, "scope_type") != string(quota.ScopeTenant) {
		return TenantUsageReservoir{}, false
	}
	out := TenantUsageReservoir{
		TenantID:      stringAttr(row, "scope_id"),
		Metric:        stringAttr(row, "metric"),
		Period:        stringAttr(row, "period"),
		Cap:           intAttr(row, "cap"),
		Available:     intAttr(row, "available"),
		Reserved:      intAttr(row, "reserved"),
		Committed:     intAttr(row, "committed"),
		Released:      intAttr(row, "released"),
		State:         stringAttr(row, "state"),
		PolicyID:      stringAttr(row, "policy_id"),
		PolicyVersion: intAttr(row, "policy_version"),
		CreatedAt:     timeAttr(row, "created_at"),
		UpdatedAt:     timeAttr(row, "updated_at"),
		Materialized:  true,
	}
	if out.TenantID == "" || out.Metric == "" || out.Period == "" {
		return TenantUsageReservoir{}, false
	}
	return out, true
}

func sortTenantUsageReservoirs(rows []TenantUsageReservoir) {
	sort.SliceStable(rows, func(i, j int) bool {
		li, lj := usageMetricRank(rows[i].Metric), usageMetricRank(rows[j].Metric)
		if li != lj {
			return li < lj
		}
		if rows[i].Period != rows[j].Period {
			return rows[i].Period > rows[j].Period
		}
		return rows[i].Metric < rows[j].Metric
	})
}

func usageMetricRank(metric string) int {
	switch metric {
	case string(quota.CostMicroUSD):
		return 0
	case string(quota.GeneratedOutputs):
		return 1
	case string(quota.Requests):
		return 2
	case string(quota.StorageBytes):
		return 3
	default:
		return 9
	}
}
