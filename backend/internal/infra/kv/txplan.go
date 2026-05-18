package kv

import "context"

// TxOpName labels one op inside a TxPlan. Classifiers switch on names so
// cancellation diagnosis survives reordering of the underlying ops.
type TxOpName string

// NamedTxOp pairs a write op with the name it answers to when DynamoDB
// reports cancellation reasons.
type NamedTxOp struct {
	Name TxOpName
	Op   WriteOp
}

// TxPlan is an ordered, named description of a TransactWrite. Plan.Name is
// the workflow label (used in logs); Ops carries the per-slot names. The
// declaration order is the wire order: DynamoDB cancellation reasons map 1:1
// to Ops by index.
type TxPlan struct {
	Name string
	Ops  []NamedTxOp
}

// Execute submits the plan's ops to KV.TransactWrite in declaration order.
func (p TxPlan) Execute(ctx context.Context, k KV) error {
	ops := make([]WriteOp, len(p.Ops))
	for i, n := range p.Ops {
		ops[i] = n.Op
	}
	return k.TransactWrite(ctx, ops)
}

// ClassifyByName maps the cancelling slot in a TxnError back to the TxOpName
// the plan assigned it. conditionFailed is true when the cancelling slot
// failed its condition expression (vs e.g. a validation error elsewhere in
// the transaction); when no slot reports a condition failure, an empty name
// and false are returned so the caller can fall back to a default class.
//
// Index-by-position is unavoidable at the wire (DynamoDB reports
// CancellationReasons[i]); the value of TxPlan is that callers never write a
// switch over those indices — they switch over names declared next to the
// op.
func ClassifyByName(plan TxPlan, err TxnError) (TxOpName, bool) {
	if err == nil {
		return "", false
	}
	items := err.Items()
	for i, it := range items {
		if !it.ConditionFailed {
			continue
		}
		if i >= len(plan.Ops) {
			return "", true
		}
		return plan.Ops[i].Name, true
	}
	return "", false
}
