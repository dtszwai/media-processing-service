// Package dynamodb is the DynamoDB driver for the kv port.
package dynamodb

import (
	"context"
	"errors"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// Table is the DynamoDB driver implementing kv.KV.
type Table struct {
	c    *dynamodb.Client
	name string
}

// New binds the driver to a single table.
func New(c *dynamodb.Client, name string) *Table {
	return &Table{c: c, name: name}
}

func (t *Table) Put(ctx context.Context, item kv.Item, opts kv.PutOptions) error {
	av, err := attributevalue.MarshalMap(item)
	if err != nil {
		return err
	}
	vals, err := marshalValues(opts.ExpressionAttributeValues)
	if err != nil {
		return err
	}
	in := &dynamodb.PutItemInput{
		TableName:                 aws.String(t.name),
		Item:                      av,
		ExpressionAttributeNames:  opts.ExpressionAttributeNames,
		ExpressionAttributeValues: vals,
	}
	if opts.ConditionExpression != "" {
		in.ConditionExpression = aws.String(opts.ConditionExpression)
	}
	_, err = t.c.PutItem(ctx, in)
	return classify(err)
}

func (t *Table) Get(ctx context.Context, key kv.Key, out any) error {
	resp, err := t.c.GetItem(ctx, &dynamodb.GetItemInput{
		TableName:      aws.String(t.name),
		ConsistentRead: aws.Bool(true),
		Key:            keyAV(key),
	})
	if err != nil {
		return classify(err)
	}
	if len(resp.Item) == 0 {
		return kv.ErrNotFound
	}
	return attributevalue.UnmarshalMap(resp.Item, out)
}

func (t *Table) Query(ctx context.Context, req kv.QueryRequest) (kv.QueryResult, error) {
	vals, err := marshalValues(req.ExpressionAttributeValues)
	if err != nil {
		return kv.QueryResult{}, err
	}
	in := &dynamodb.QueryInput{
		TableName:                 aws.String(t.name),
		KeyConditionExpression:    aws.String(req.KeyConditionExpression),
		ExpressionAttributeNames:  req.ExpressionAttributeNames,
		ExpressionAttributeValues: vals,
	}
	if req.Index != "" {
		in.IndexName = aws.String(req.Index)
	}
	if req.FilterExpression != "" {
		in.FilterExpression = aws.String(req.FilterExpression)
	}
	if req.ProjectionExpression != "" {
		in.ProjectionExpression = aws.String(req.ProjectionExpression)
	}
	if req.Limit > 0 {
		in.Limit = aws.Int32(req.Limit)
	}
	if req.ConsistentRead {
		in.ConsistentRead = aws.Bool(true)
	}
	if req.ExclusiveStartKey != nil {
		in.ExclusiveStartKey = keyAV(*req.ExclusiveStartKey)
	}
	if req.ScanIndexForward != nil {
		in.ScanIndexForward = req.ScanIndexForward
	}
	resp, err := t.c.Query(ctx, in)
	if err != nil {
		return kv.QueryResult{}, classify(err)
	}
	items := make([]kv.Row, 0, len(resp.Items))
	for _, raw := range resp.Items {
		items = append(items, row{av: raw})
	}
	out := kv.QueryResult{Items: items}
	if len(resp.LastEvaluatedKey) > 0 {
		k := avKey(resp.LastEvaluatedKey)
		out.LastEvaluatedKey = &k
	}
	return out, nil
}

func (t *Table) Update(ctx context.Context, op kv.UpdateOp) error {
	in, err := t.buildUpdate(op)
	if err != nil {
		return err
	}
	_, err = t.c.UpdateItem(ctx, in)
	return classify(err)
}

func (t *Table) UpdateReturning(ctx context.Context, op kv.UpdateOp) (kv.UpdateOutput, error) {
	in, err := t.buildUpdate(op)
	if err != nil {
		return kv.UpdateOutput{}, err
	}
	in.ReturnValues = types.ReturnValueAllNew
	resp, err := t.c.UpdateItem(ctx, in)
	if err != nil {
		return kv.UpdateOutput{}, classify(err)
	}
	attrs := map[string]any{}
	if len(resp.Attributes) > 0 {
		if uerr := attributevalue.UnmarshalMap(resp.Attributes, &attrs); uerr != nil {
			return kv.UpdateOutput{}, uerr
		}
	}
	return kv.UpdateOutput{Attributes: attrs}, nil
}

func (t *Table) buildUpdate(op kv.UpdateOp) (*dynamodb.UpdateItemInput, error) {
	vals, err := marshalValues(op.ExpressionAttributeValues)
	if err != nil {
		return nil, err
	}
	in := &dynamodb.UpdateItemInput{
		TableName:                 aws.String(t.name),
		Key:                       keyAV(op.Key),
		ExpressionAttributeNames:  op.ExpressionAttributeNames,
		ExpressionAttributeValues: vals,
	}
	if op.UpdateExpression != "" {
		in.UpdateExpression = aws.String(op.UpdateExpression)
	}
	if op.ConditionExpression != "" {
		in.ConditionExpression = aws.String(op.ConditionExpression)
	}
	return in, nil
}

func (t *Table) Delete(ctx context.Context, op kv.DeleteOp) error {
	vals, err := marshalValues(op.ExpressionAttributeValues)
	if err != nil {
		return err
	}
	in := &dynamodb.DeleteItemInput{
		TableName:                 aws.String(t.name),
		Key:                       keyAV(op.Key),
		ExpressionAttributeNames:  op.ExpressionAttributeNames,
		ExpressionAttributeValues: vals,
	}
	if op.ConditionExpression != "" {
		in.ConditionExpression = aws.String(op.ConditionExpression)
	}
	_, err = t.c.DeleteItem(ctx, in)
	return classify(err)
}

func (t *Table) TransactWrite(ctx context.Context, ops []kv.WriteOp) error {
	items := make([]types.TransactWriteItem, 0, len(ops))
	for _, op := range ops {
		item, err := t.toTxnItem(op)
		if err != nil {
			return err
		}
		items = append(items, item)
	}
	_, err := t.c.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: items,
	})
	return classifyTxn(err)
}

func (t *Table) toTxnItem(op kv.WriteOp) (types.TransactWriteItem, error) {
	switch {
	case op.Put != nil:
		av, err := attributevalue.MarshalMap(op.Put.Item)
		if err != nil {
			return types.TransactWriteItem{}, err
		}
		vals, err := marshalValues(op.Put.ExpressionAttributeValues)
		if err != nil {
			return types.TransactWriteItem{}, err
		}
		put := &types.Put{
			TableName:                 aws.String(t.name),
			Item:                      av,
			ExpressionAttributeNames:  op.Put.ExpressionAttributeNames,
			ExpressionAttributeValues: vals,
		}
		if op.Put.ConditionExpression != "" {
			put.ConditionExpression = aws.String(op.Put.ConditionExpression)
		}
		return types.TransactWriteItem{Put: put}, nil
	case op.Update != nil:
		vals, err := marshalValues(op.Update.ExpressionAttributeValues)
		if err != nil {
			return types.TransactWriteItem{}, err
		}
		upd := &types.Update{
			TableName:                 aws.String(t.name),
			Key:                       keyAV(op.Update.Key),
			ExpressionAttributeNames:  op.Update.ExpressionAttributeNames,
			ExpressionAttributeValues: vals,
		}
		if op.Update.UpdateExpression != "" {
			upd.UpdateExpression = aws.String(op.Update.UpdateExpression)
		}
		if op.Update.ConditionExpression != "" {
			upd.ConditionExpression = aws.String(op.Update.ConditionExpression)
		}
		return types.TransactWriteItem{Update: upd}, nil
	case op.Delete != nil:
		vals, err := marshalValues(op.Delete.ExpressionAttributeValues)
		if err != nil {
			return types.TransactWriteItem{}, err
		}
		del := &types.Delete{
			TableName:                 aws.String(t.name),
			Key:                       keyAV(op.Delete.Key),
			ExpressionAttributeNames:  op.Delete.ExpressionAttributeNames,
			ExpressionAttributeValues: vals,
		}
		if op.Delete.ConditionExpression != "" {
			del.ConditionExpression = aws.String(op.Delete.ConditionExpression)
		}
		return types.TransactWriteItem{Delete: del}, nil
	default:
		return types.TransactWriteItem{}, errors.New("kv: empty WriteOp")
	}
}

func keyAV(k kv.Key) map[string]types.AttributeValue {
	out := map[string]types.AttributeValue{
		"PK": &types.AttributeValueMemberS{Value: k.PK},
		"SK": &types.AttributeValueMemberS{Value: k.SK},
	}
	// Emit extra attributes so GSI pagination tokens round-trip correctly.
	// DynamoDB requires all key attributes (table PK+SK + GSI PK+SK) in the
	// ExclusiveStartKey for a GSI query.
	for attr, val := range k.ExtraAttrs {
		out[attr] = &types.AttributeValueMemberS{Value: val}
	}
	return out
}

func avKey(in map[string]types.AttributeValue) kv.Key {
	out := kv.Key{}
	if v, ok := in["PK"].(*types.AttributeValueMemberS); ok {
		out.PK = v.Value
	}
	if v, ok := in["SK"].(*types.AttributeValueMemberS); ok {
		out.SK = v.Value
	}
	// Capture any additional string attributes from the DynamoDB key token so
	// that GSI pagination cursors carry the full 4-attribute key on the next page.
	for name, av := range in {
		if name == "PK" || name == "SK" {
			continue
		}
		if v, ok := av.(*types.AttributeValueMemberS); ok {
			if out.ExtraAttrs == nil {
				out.ExtraAttrs = make(map[string]string)
			}
			out.ExtraAttrs[name] = v.Value
		}
	}
	return out
}

func marshalValues(in kv.Values) (map[string]types.AttributeValue, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]types.AttributeValue, len(in))
	for k, v := range in {
		av, err := attributevalue.Marshal(v)
		if err != nil {
			return nil, err
		}
		out[k] = av
	}
	return out, nil
}
