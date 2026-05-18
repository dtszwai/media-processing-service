package dynamodb

import (
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// row wraps a DDB attribute-value map so callers can decode rows into structs
// without seeing AttributeValue types. Get returns the most natural Go value
// for the attribute kind (string/[]byte/float64/bool/map/slice/nil).
type row struct {
	av map[string]types.AttributeValue
}

func (r row) Unmarshal(out any) error {
	return attributevalue.UnmarshalMap(r.av, out)
}

func (r row) Get(name string) any { return unwrapAV(r.av[name]) }

// unwrapAV converts one AttributeValue to its natural Go representation. Map
// and List recurse so callers get plain Go structures all the way down.
func unwrapAV(v types.AttributeValue) any {
	switch x := v.(type) {
	case nil, *types.AttributeValueMemberNULL:
		return nil
	case *types.AttributeValueMemberS:
		return x.Value
	case *types.AttributeValueMemberN:
		return x.Value
	case *types.AttributeValueMemberB:
		return x.Value
	case *types.AttributeValueMemberBOOL:
		return x.Value
	case *types.AttributeValueMemberM:
		out := make(map[string]any, len(x.Value))
		for k, mv := range x.Value {
			out[k] = unwrapAV(mv)
		}
		return out
	case *types.AttributeValueMemberL:
		out := make([]any, 0, len(x.Value))
		for _, lv := range x.Value {
			out = append(out, unwrapAV(lv))
		}
		return out
	}
	return nil
}
