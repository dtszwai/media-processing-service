package ops

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// stringAttr / intAttr / boolAttr / timeAttr defensively pull a typed value
// out of a row decoded into map[string]any. The on-disk row is heterogenous
// (some attributes are stored as numbers, some as strings) so a single typed
// helper per shape keeps the FullJobView assembly readable.

func stringAttr(row map[string]any, name string) string {
	v, ok := row[name]
	if !ok {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	case fmt.Stringer:
		return t.String()
	}
	return ""
}

func intAttr(row map[string]any, name string) int64 {
	v, ok := row[name]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case int:
		return int64(t)
	case int32:
		return int64(t)
	case int64:
		return t
	case float64:
		return int64(t)
	case json.Number:
		i, _ := t.Int64()
		return i
	case string:
		var n int64
		_, err := fmt.Sscanf(t, "%d", &n)
		if err != nil {
			return 0
		}
		return n
	}
	return 0
}

func boolAttr(row map[string]any, name string) bool {
	v, ok := row[name]
	if !ok {
		return false
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false
}

func timeAttr(row map[string]any, name string) time.Time {
	s := stringAttr(row, name)
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

// encodeCursor / decodeCursor wrap a DynamoDB LastEvaluatedKey into a
// base64-url string the client passes back verbatim. The contents are
// driver-shaped (attribute-value envelope); keeping them opaque on the
// client lets the server change row layouts without contract churn.
func encodeCursor(key map[string]types.AttributeValue) string {
	if len(key) == 0 {
		return ""
	}
	enc := map[string]json.RawMessage{}
	for k, v := range key {
		b, err := marshalAttr(v)
		if err != nil {
			return ""
		}
		enc[k] = b
	}
	raw, err := json.Marshal(enc)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(cursor string) (map[string]types.AttributeValue, error) {
	if cursor == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return nil, fmt.Errorf("ops: cursor decode: %w", err)
	}
	envelope := map[string]json.RawMessage{}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("ops: cursor unmarshal: %w", err)
	}
	out := make(map[string]types.AttributeValue, len(envelope))
	for k, v := range envelope {
		av, err := unmarshalAttr(v)
		if err != nil {
			return nil, fmt.Errorf("ops: cursor attr %q: %w", k, err)
		}
		out[k] = av
	}
	return out, nil
}

// avToAny is the inverse of attributevalue.Marshal — it decodes a single
// DynamoDB AttributeValue into a plain Go value for ad-hoc reads in the ops
// surface. The console renders heterogeneous rows; calling
// attributevalue.UnmarshalMap into a target struct would lose attributes
// the struct doesn't declare.
func avToAny(av types.AttributeValue) any {
	switch t := av.(type) {
	case *types.AttributeValueMemberS:
		return t.Value
	case *types.AttributeValueMemberN:
		return t.Value
	case *types.AttributeValueMemberBOOL:
		return t.Value
	case *types.AttributeValueMemberB:
		return t.Value
	case *types.AttributeValueMemberSS:
		return t.Value
	case *types.AttributeValueMemberNS:
		return t.Value
	case *types.AttributeValueMemberL:
		out := make([]any, len(t.Value))
		for i, v := range t.Value {
			out[i] = avToAny(v)
		}
		return out
	case *types.AttributeValueMemberM:
		out := map[string]any{}
		for k, v := range t.Value {
			out[k] = avToAny(v)
		}
		return out
	case *types.AttributeValueMemberNULL:
		return nil
	}
	return nil
}

type attrEnvelope struct {
	Kind  string `json:"k"`
	Value string `json:"v"`
}

func marshalAttr(av types.AttributeValue) (json.RawMessage, error) {
	switch t := av.(type) {
	case *types.AttributeValueMemberS:
		return json.Marshal(attrEnvelope{Kind: "S", Value: t.Value})
	case *types.AttributeValueMemberN:
		return json.Marshal(attrEnvelope{Kind: "N", Value: t.Value})
	case *types.AttributeValueMemberB:
		return json.Marshal(attrEnvelope{Kind: "B", Value: base64.StdEncoding.EncodeToString(t.Value)})
	case *types.AttributeValueMemberBOOL:
		v := "false"
		if t.Value {
			v = "true"
		}
		return json.Marshal(attrEnvelope{Kind: "BOOL", Value: v})
	}
	return nil, fmt.Errorf("ops: cursor: unsupported attribute %T", av)
}

func unmarshalAttr(raw json.RawMessage) (types.AttributeValue, error) {
	var env attrEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	switch env.Kind {
	case "S":
		return &types.AttributeValueMemberS{Value: env.Value}, nil
	case "N":
		return &types.AttributeValueMemberN{Value: env.Value}, nil
	case "B":
		b, err := base64.StdEncoding.DecodeString(env.Value)
		if err != nil {
			return nil, err
		}
		return &types.AttributeValueMemberB{Value: b}, nil
	case "BOOL":
		return &types.AttributeValueMemberBOOL{Value: env.Value == "true"}, nil
	}
	return nil, fmt.Errorf("ops: cursor: unsupported kind %q", env.Kind)
}
