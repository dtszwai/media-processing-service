package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// DdbRow is the raw projection of one item the operator inspects. The
// console renders attributes as a key/value grid; the AttributeValue → Go
// map encoding here keeps the surface stable as the schema evolves.
type DdbRow struct {
	PK         string
	SK         string
	ItemType   string
	Attributes map[string]any
}

// ScanDdbFilter narrows the table scan. pk_prefix matches against PK with
// begins_with(); sk_prefix is only honored when pk_prefix is a complete
// partition key.
type ScanDdbFilter struct {
	PKPrefix string
	SKPrefix string
	Limit    int32
	Cursor   string
}

// ScanDdb returns one page of rows whose PK begins with PKPrefix and whose
// SK begins with SKPrefix (either is optional). The LOCAL_ONLY console
// always uses Scan with begins_with rather than guessing whether the prefix
// is a complete partition key; heuristics get the (PK, SK) shape wrong for
// 3-segment partition keys like AUDIT#GATE#<jobID> and OUTBOX_CHECKPOINT#…,
// silently returning zero rows on the follow-up page. At console scale the
// Scan latency is acceptable.
func (s *Service) ScanDdb(ctx context.Context, f ScanDdbFilter) ([]DdbRow, string, error) {
	if s.DDB == nil {
		return nil, "", fmt.Errorf("ops: ddb client not wired")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}
	in := &dynamodb.ScanInput{
		TableName: aws.String(s.Table),
		Limit:     aws.Int32(limit),
	}
	values := map[string]types.AttributeValue{}
	filters := []string{}
	if f.PKPrefix != "" {
		filters = append(filters, "begins_with(PK, :p)")
		values[":p"] = &types.AttributeValueMemberS{Value: f.PKPrefix}
	}
	if f.SKPrefix != "" {
		filters = append(filters, "begins_with(SK, :s)")
		values[":s"] = &types.AttributeValueMemberS{Value: f.SKPrefix}
	}
	if len(filters) > 0 {
		in.FilterExpression = aws.String(strings.Join(filters, " AND "))
		in.ExpressionAttributeValues = values
	}
	if f.Cursor != "" {
		key, err := decodeCursor(f.Cursor)
		if err != nil {
			return nil, "", err
		}
		in.ExclusiveStartKey = key
	}
	out, err := s.DDB.Scan(ctx, in)
	if err != nil {
		return nil, "", err
	}
	rows := make([]DdbRow, 0, len(out.Items))
	for _, av := range out.Items {
		rows = append(rows, decodeDdbRow(av))
	}
	cursor := ""
	if out.LastEvaluatedKey != nil {
		cursor = encodeCursor(out.LastEvaluatedKey)
	}
	return rows, cursor, nil
}

func decodeDdbRow(av map[string]types.AttributeValue) DdbRow {
	row := DdbRow{Attributes: map[string]any{}}
	for k, v := range av {
		row.Attributes[k] = avToAny(v)
	}
	row.PK = stringAttr(row.Attributes, "PK")
	row.SK = stringAttr(row.Attributes, "SK")
	row.ItemType = stringAttr(row.Attributes, "item_type")
	return row
}

// GetDdbRow loads one row by composite key.
func (s *Service) GetDdbRow(ctx context.Context, pk, sk string) (*DdbRow, error) {
	if pk == "" || sk == "" {
		return nil, fmt.Errorf("ops: pk + sk required")
	}
	var row map[string]any
	if err := s.KV.Get(ctx, kv.Key{PK: pk, SK: sk}, &row); err != nil {
		return nil, err
	}
	return &DdbRow{
		PK:         pk,
		SK:         sk,
		ItemType:   stringAttr(row, "item_type"),
		Attributes: row,
	}, nil
}

// PutDdbAttr applies a single attribute mutation. value_json is decoded
// into a Go value, then converted to an AttributeValue. Audited.
//
// AUDIT# rows are refused so the audit invariant from AGENTS.md
// ("Audit rows are immutable") survives the operator surface. The console
// can still SHOW audit rows; it just cannot rewrite them.
func (s *Service) PutDdbAttr(ctx context.Context, pk, sk, name, valueJSON string) error {
	if pk == "" || sk == "" || name == "" {
		return fmt.Errorf("ops: pk + sk + attribute_name required")
	}
	if name == "PK" || name == "SK" {
		return fmt.Errorf("ops: refusing to mutate PK/SK")
	}
	if strings.HasPrefix(pk, "AUDIT#") {
		return fmt.Errorf("ops: audit rows are immutable (AGENTS.md invariant)")
	}
	var decoded any
	if err := json.Unmarshal([]byte(valueJSON), &decoded); err != nil {
		return fmt.Errorf("ops: value_json: %w", err)
	}
	av, err := goToAttributeValue(decoded)
	if err != nil {
		return err
	}
	_, err = s.DDB.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.Table),
		Key: map[string]types.AttributeValue{
			"PK": &types.AttributeValueMemberS{Value: pk},
			"SK": &types.AttributeValueMemberS{Value: sk},
		},
		UpdateExpression:         aws.String("SET #n = :v"),
		ExpressionAttributeNames: map[string]string{"#n": name},
		ExpressionAttributeValues: map[string]types.AttributeValue{
			":v": av,
		},
	})
	if err != nil {
		return fmt.Errorf("ops: put attr: %w", err)
	}
	s.audit(ctx, AuditEvent{
		Operation: "PutDdbAttr",
		Target:    pk + "/" + sk,
		Details:   map[string]string{"attribute": name, "value_json": valueJSON},
	})
	return nil
}

// DeleteDdbRow removes a single row by composite key. Audited.
// AUDIT# rows are refused so the audit invariant from AGENTS.md survives
// the operator surface — the console can read audit rows but never delete
// them.
func (s *Service) DeleteDdbRow(ctx context.Context, pk, sk string) error {
	if pk == "" || sk == "" {
		return fmt.Errorf("ops: pk + sk required")
	}
	if strings.HasPrefix(pk, "AUDIT#") {
		return fmt.Errorf("ops: audit rows are immutable (AGENTS.md invariant)")
	}
	if err := s.KV.Delete(ctx, kv.DeleteOp{Key: kv.Key{PK: pk, SK: sk}}); err != nil {
		return err
	}
	s.audit(ctx, AuditEvent{
		Operation: "DeleteDdbRow",
		Target:    pk + "/" + sk,
	})
	return nil
}

// goToAttributeValue converts a JSON-decoded Go value into a DynamoDB
// AttributeValue. Numbers always encode as N; bools as BOOL. Mixed maps
// become M, slices become L. nil → NULL.
func goToAttributeValue(v any) (types.AttributeValue, error) {
	switch t := v.(type) {
	case nil:
		return &types.AttributeValueMemberNULL{Value: true}, nil
	case string:
		return &types.AttributeValueMemberS{Value: t}, nil
	case bool:
		return &types.AttributeValueMemberBOOL{Value: t}, nil
	case float64:
		// json.Unmarshal decodes numbers as float64 by default; the N
		// AttributeValue is a string-encoded number so this preserves
		// precision for the common integer case.
		return &types.AttributeValueMemberN{Value: trimFloat(t)}, nil
	case json.Number:
		return &types.AttributeValueMemberN{Value: t.String()}, nil
	case []any:
		out := make([]types.AttributeValue, len(t))
		for i, el := range t {
			child, err := goToAttributeValue(el)
			if err != nil {
				return nil, err
			}
			out[i] = child
		}
		return &types.AttributeValueMemberL{Value: out}, nil
	case map[string]any:
		out := make(map[string]types.AttributeValue, len(t))
		for k, el := range t {
			child, err := goToAttributeValue(el)
			if err != nil {
				return nil, err
			}
			out[k] = child
		}
		return &types.AttributeValueMemberM{Value: out}, nil
	}
	return nil, fmt.Errorf("ops: unsupported attribute type %T", v)
}

func trimFloat(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}
