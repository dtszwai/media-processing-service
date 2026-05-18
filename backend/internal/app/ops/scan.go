package ops

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

func normalizedListLimit(limit int32) int32 {
	if limit <= 0 {
		return defaultListLimit
	}
	if limit > maxListLimit {
		return maxListLimit
	}
	return limit
}

func matchesOptional(want, got string) bool {
	return want == "" || want == got
}

func scanUntilLimit[T any](
	ctx context.Context,
	client *dynamodb.Client,
	in *dynamodb.ScanInput,
	cursor string,
	limit int32,
	decode func(map[string]types.AttributeValue) (T, bool),
) ([]T, string, error) {
	limit = normalizedListLimit(limit)
	in.Limit = aws.Int32(limit * 4)
	if cursor != "" {
		key, err := decodeCursor(cursor)
		if err != nil {
			return nil, "", err
		}
		in.ExclusiveStartKey = key
	}

	rows := make([]T, 0, limit)
	var nextKey map[string]types.AttributeValue
	for len(rows) < int(limit) {
		out, err := client.Scan(ctx, in)
		if err != nil {
			return nil, "", err
		}
		for _, av := range out.Items {
			if row, ok := decode(av); ok {
				rows = append(rows, row)
			}
		}
		if out.LastEvaluatedKey == nil {
			nextKey = nil
			break
		}
		in.ExclusiveStartKey = out.LastEvaluatedKey
		nextKey = out.LastEvaluatedKey
	}

	if len(rows) > int(limit) {
		rows = rows[:limit]
	}
	if nextKey == nil {
		return rows, "", nil
	}
	return rows, encodeCursor(nextKey), nil
}
