package dynamodb

import (
	"errors"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func classify(err error) error {
	if err == nil {
		return nil
	}
	var cfe *types.ConditionalCheckFailedException
	if errors.As(err, &cfe) {
		return errors.Join(kv.ErrConditionFailed, err)
	}
	return err
}

type txnError struct {
	err   error
	items []kv.ItemCancelReason
}

func (e *txnError) Error() string                { return e.err.Error() }
func (e *txnError) Unwrap() error                { return e.err }
func (e *txnError) Items() []kv.ItemCancelReason { return e.items }

func classifyTxn(err error) error {
	if err == nil {
		return nil
	}
	var tce *types.TransactionCanceledException
	if !errors.As(err, &tce) {
		return classify(err)
	}
	items := make([]kv.ItemCancelReason, len(tce.CancellationReasons))
	hasCond := false
	for i, r := range tce.CancellationReasons {
		ir := kv.ItemCancelReason{}
		if r.Code != nil {
			ir.Code = *r.Code
			if *r.Code == "ConditionalCheckFailed" {
				ir.ConditionFailed = true
				hasCond = true
			}
		}
		if r.Message != nil {
			ir.Message = *r.Message
		}
		items[i] = ir
	}
	wrapped := error(tce)
	if hasCond {
		wrapped = errors.Join(kv.ErrConditionFailed, tce)
	}
	return &txnError{err: wrapped, items: items}
}
