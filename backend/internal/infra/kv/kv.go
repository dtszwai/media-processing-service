// Package kv is the data-store port. Drivers (infra/kv/<vendor>/) implement it.
//
// The port keeps DDB-flavored expression strings opaque so app callers can pass
// them through without inventing a SQL AST. Values are plain Go types
// (string/[]byte/intN/bool/map/slice); drivers translate them to wire types.
package kv

import (
	"context"
	"errors"
)

var (
	// ErrNotFound is returned when a Get finds no row.
	ErrNotFound = errors.New("kv: not found")
	// ErrConditionFailed wraps the driver's conditional-check failure.
	ErrConditionFailed = errors.New("kv: conditional check failed")
)

// Key is a composite (PK, SK) row key. ExtraAttrs carries additional
// attributes required by GSI pagination tokens (a DynamoDB GSI
// LastEvaluatedKey includes the table's PK+SK plus the GSI's own key
// attributes). Callers treat Key as opaque — only drivers read/write
// ExtraAttrs.
type Key struct {
	PK         string
	SK         string
	ExtraAttrs map[string]string
}

// Item is anything marshalable to a row: a struct with dynamodbav tags or a
// raw map[string]any. The driver normalizes both.
type Item = any

// Values maps placeholders to Go values for ExpressionAttributeValues.
type Values = map[string]any

// Names maps placeholders to attribute names for ExpressionAttributeNames.
type Names = map[string]string

// PutOptions controls a Put.
type PutOptions struct {
	ConditionExpression       string
	ExpressionAttributeNames  Names
	ExpressionAttributeValues Values
}

// PutOp is a Put inside a TransactWrite.
type PutOp struct {
	Item                      Item
	ConditionExpression       string
	ExpressionAttributeNames  Names
	ExpressionAttributeValues Values
}

// UpdateOp drives Update or one slot of TransactWrite.
type UpdateOp struct {
	Key                       Key
	ConditionExpression       string
	UpdateExpression          string
	ExpressionAttributeNames  Names
	ExpressionAttributeValues Values
}

// DeleteOp drives Delete or one slot of TransactWrite.
type DeleteOp struct {
	Key                       Key
	ConditionExpression       string
	ExpressionAttributeNames  Names
	ExpressionAttributeValues Values
}

// WriteOp is one slot in a TransactWrite. Exactly one of Put/Update/Delete is non-nil.
type WriteOp struct {
	Put    *PutOp
	Update *UpdateOp
	Delete *DeleteOp
}

// UpdateOutput optionally carries the post-update row (ReturnAllNew).
type UpdateOutput struct {
	Attributes map[string]any
}

// QueryRequest describes a Query against the base table or a GSI.
type QueryRequest struct {
	Index                     string // "" = base table
	KeyConditionExpression    string
	FilterExpression          string
	ProjectionExpression      string
	ExpressionAttributeNames  Names
	ExpressionAttributeValues Values
	Limit                     int32
	ConsistentRead            bool
	ExclusiveStartKey         *Key
	// ScanIndexForward controls sort order. nil preserves DynamoDB's default
	// (ascending). Set to false for newest-first queries against time-sorted GSIs.
	ScanIndexForward *bool
}

// Row is one Query result item. Unmarshal decodes the row into a struct (with
// dynamodbav tags). Get returns the raw Go value (string/[]byte/float64/bool/…)
// for ad-hoc reads; for typed reads always prefer Unmarshal.
type Row interface {
	Unmarshal(out any) error
	Get(name string) any
}

// QueryResult is one page of items plus the resume key, if any.
type QueryResult struct {
	Items            []Row
	LastEvaluatedKey *Key
}

// ItemCancelReason is one slot in a TransactWrite cancellation. Slots correspond
// 1:1 to ops in submission order; ConditionFailed=true marks the cancelling slot.
type ItemCancelReason struct {
	ConditionFailed bool
	Code            string
	Message         string
}

// TxnError exposes per-op cancellation reasons after a TransactWrite cancels.
type TxnError interface {
	error
	Items() []ItemCancelReason
}

// KV is the data-store port.
type KV interface {
	Put(ctx context.Context, item Item, opts PutOptions) error
	Get(ctx context.Context, key Key, out any) error
	Query(ctx context.Context, req QueryRequest) (QueryResult, error)
	Update(ctx context.Context, op UpdateOp) error
	UpdateReturning(ctx context.Context, op UpdateOp) (UpdateOutput, error)
	Delete(ctx context.Context, op DeleteOp) error
	TransactWrite(ctx context.Context, ops []WriteOp) error
}
