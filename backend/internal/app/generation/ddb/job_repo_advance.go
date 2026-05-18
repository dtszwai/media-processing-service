package ddb

import (
	"context"
	"errors"
	"strings"
	"time"

	genapp "github.com/dtszwai/media-processing-service/backend/internal/app/generation"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/app/outbox"
	quotaapp "github.com/dtszwai/media-processing-service/backend/internal/app/quota"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// Op names internal to AdvanceStageAndEnqueue. They are TxPlan slots whose cancellation does not classify a RetryClass on its own — they exist so failure logs name the slot rather than its index.
const (
	opFlipTerminalMedia    kv.TxOpName = "flip_terminal_media_lifecycle"
	opFailGenerationRecord kv.TxOpName = "fail_generation_record"
	opFailOutputRecord     kv.TxOpName = "fail_output_record"
	opAuditGateDecision    kv.TxOpName = "audit_gate_decision"
	opAuditGateEvent       kv.TxOpName = "audit_gate_event"
)

// AdvanceStageAndEnqueue advances the stage, applies every stage-produced mutation, and writes the outbox row in one transaction. Each op carries an explicit name so cancellation reasons map to behaviour through the name — not the slot index. The plan label "workflow.advance_stage" is the observability hook for the whole transaction.
func (r *JobRepo) AdvanceStageAndEnqueue(ctx context.Context, job *generation.Job, result genapp.StageResult) error {
	now := time.Now().UTC()
	plan := kv.TxPlan{
		Name: "workflow.advance_stage",
		Ops: []kv.NamedTxOp{{
			Name: quotaapp.OpAdvanceJobStage,
			Op:   kv.WriteOp{Update: r.buildAdvanceJob(job, result, now)},
		}, {
			Name: "workflow.put_stage_attempt",
			Op: kv.WriteOp{Put: &kv.PutOp{
				Item:                buildStageAttemptItem(job, result, now, genapp.TraceparentFromContext(ctx)),
				ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
			}},
		}},
	}

	if len(result.OutboxBody) > 0 {
		plan.Ops = append(plan.Ops, kv.NamedTxOp{
			Name: quotaapp.OpPutOutboxNextStage,
			Op: kv.WriteOp{Put: &kv.PutOp{
				Item: outbox.JobItem(outbox.JobRow{
					JobID:         job.ID,
					TenantID:      job.TenantID,
					TenantLane:    genapp.TenantLane(job.TenantID),
					Tier:          string(job.Tier),
					Stage:         string(result.NextStage),
					ResourceClass: string(result.ResourceClass),
					Body:          result.OutboxBody,
					PartitionTS:   now,
				}),
				ConditionExpression: "attribute_not_exists(PK)",
			}},
		})
	}

	if result.NextStage == genapp.StageTerminal {
		plan.Ops = append(plan.Ops, kv.NamedTxOp{
			Name: opFlipTerminalMedia,
			Op:   kv.WriteOp{Update: r.buildTerminalMediaUpdate(job, result, now)},
		}, kv.NamedTxOp{
			Name: "workflow.put_terminal",
			Op: kv.WriteOp{Put: &kv.PutOp{
				Item:                buildTerminalItem(job, result, now),
				ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
			}},
		})
		if result.TerminalError != nil {
			failOps := failGenerationOutputOps(*job, result.TerminalError, now)
			plan.Ops = append(plan.Ops,
				kv.NamedTxOp{Name: opFailGenerationRecord, Op: failOps[0]},
				kv.NamedTxOp{Name: opFailOutputRecord, Op: failOps[1]},
			)
		}
	}

	if result.GateDecision != nil {
		plan.Ops = append(plan.Ops,
			kv.NamedTxOp{
				Name: opAuditGateDecision,
				Op: kv.WriteOp{Put: &kv.PutOp{
					Item:                buildAuditItem(job, *result.GateDecision, now),
					ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
				}},
			},
			kv.NamedTxOp{
				Name: opAuditGateEvent,
				Op: kv.WriteOp{Put: &kv.PutOp{
					Item:                BuildGateAuditEventRow(*result.GateDecision, now),
					ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
				}},
			},
		)
	}

	// LedgerOp.Items is contractually [aggregate, ledger] (see genapp.LedgerOp doc). The names attached here are what quotaapp.ClassifyTxnError keys off when DynamoDB cancels a slot.
	if result.LedgerOp != nil {
		if len(result.LedgerOp.Items) != 2 {
			return errors.New("job: LedgerOp.Items must be [aggregate, ledger] (2 items)")
		}
		aggregateName, ledgerName := ledgerOpNames(job, result)
		plan.Ops = append(plan.Ops,
			kv.NamedTxOp{Name: aggregateName, Op: result.LedgerOp.Items[0]},
			kv.NamedTxOp{Name: ledgerName, Op: result.LedgerOp.Items[1]},
		)
	}

	if err := plan.Execute(ctx, r.KV); err != nil {
		var txnErr kv.TxnError
		if errors.As(err, &txnErr) && quotaapp.ClassifyTxnError(plan, txnErr) == quotaapp.RetryExhausted {
			return generation.Terminal("BUDGET_EXHAUSTED", "tenant daily budget exhausted; provider not called")
		}
		return err
	}
	return nil
}

func ledgerOpNames(job *generation.Job, result genapp.StageResult) (kv.TxOpName, kv.TxOpName) {
	switch {
	case job.CurrentStage == generation.StageCostReserve && result.NextStage == generation.StagePromptPrepare:
		return quotaapp.OpAggregateReserve, quotaapp.OpLedgerReserve
	case result.NextStage == genapp.StageTerminal && result.TerminalError != nil:
		return quotaapp.OpAggregateRelease, quotaapp.OpLedgerRelease
	case result.NextStage == generation.StageOutputModeration:
		return quotaapp.OpAggregateCommit, quotaapp.OpLedgerCommit
	default:
		return quotaapp.OpAggregateTenantQuota, quotaapp.OpLedgerTenantQuota
	}
}

func (r *JobRepo) CancelJob(ctx context.Context, tenantID, jobID, reason string) error {
	if tenantID == "" || jobID == "" {
		return errors.New("job cancel: tenant_id and job_id required")
	}
	if reason == "" {
		reason = "OPERATOR_CANCELLED"
	}
	job, err := r.GetJob(ctx, tenantID, jobID)
	if err != nil {
		return err
	}
	switch job.Status {
	case generation.StatusComplete, generation.StatusFailed, generation.StatusCancelled:
		return errors.New("job cancel: job already terminal")
	}
	now := time.Now().UTC()
	plan := kv.TxPlan{
		Name: "workflow.cancel_job",
		Ops: []kv.NamedTxOp{{
			Name: quotaapp.OpAdvanceJobStage,
			Op: kv.WriteOp{Update: &kv.UpdateOp{
				Key:                 kv.Key{PK: JobPK(job.ID), SK: JobSK},
				ConditionExpression: "#st IN (:queued, :running, :blocked) AND stage_version = :v",
				UpdateExpression:    "SET #st = :cancelled, current_stage = :terminal, stage_version = :nv, completed_at = :now, updated_at = :now, error_code = :code, error_message = :reason, gsi_job_pk = :gpk",
				ExpressionAttributeNames: kv.Names{
					"#st": "status",
				},
				ExpressionAttributeValues: kv.Values{
					":queued":    string(generation.StatusQueued),
					":running":   string(generation.StatusRunning),
					":blocked":   string(generation.StatusBlocked),
					":cancelled": string(generation.StatusCancelled),
					":terminal":  string(generation.StageTerminal),
					":v":         job.StageVersion,
					":nv":        job.StageVersion + 1,
					":now":       now.Format(time.RFC3339Nano),
					":code":      "CANCELLED",
					":reason":    reason,
					":gpk":       "TENANT#" + job.TenantID + "#STATUS#" + string(generation.StatusCancelled),
				},
			}},
		}, {
			Name: opFlipTerminalMedia,
			Op:   kv.WriteOp{Update: mediaapp.LifecycleFlipOp(job.TenantID, job.MediaID, media.LifecycleFailed, now)},
		}, {
			Name: "workflow.put_terminal",
			Op: kv.WriteOp{Put: &kv.PutOp{
				Item:                buildCancelledTerminalItem(*job, reason, now),
				ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
			}},
		}},
	}
	outputOps := cancelGenerationOutputOps(*job, reason, now)
	plan.Ops = append(plan.Ops,
		kv.NamedTxOp{Name: opFailGenerationRecord, Op: outputOps[0]},
		kv.NamedTxOp{Name: opFailOutputRecord, Op: outputOps[1]},
	)
	releaseOps, err := r.terminalQuotaReleaseOps(job)
	if err != nil {
		return err
	}
	plan.Ops = append(plan.Ops, releaseOps...)
	return plan.Execute(ctx, r.KV)
}

// ForceFailJob applies the operator-forced FAILED terminal transition in the
// same transaction shape as other generation terminal paths.
func (r *JobRepo) ForceFailJob(ctx context.Context, tenantID, jobID, errorCode, errorMessage string) error {
	if tenantID == "" || jobID == "" {
		return errors.New("job force-fail: tenant_id and job_id required")
	}
	if errorCode == "" {
		errorCode = "OPERATOR_FORCED_FAIL"
	}
	job, err := r.GetJob(ctx, tenantID, jobID)
	if err != nil {
		return err
	}
	switch job.Status {
	case generation.StatusComplete, generation.StatusFailed, generation.StatusCancelled:
		return errors.New("job force-fail: job already terminal")
	}
	now := time.Now().UTC()
	terminalErr := &generation.Error{Code: errorCode, Message: errorMessage, Terminal: true}
	plan := kv.TxPlan{
		Name: "workflow.force_fail_job",
		Ops: []kv.NamedTxOp{{
			Name: quotaapp.OpAdvanceJobStage,
			Op: kv.WriteOp{Update: &kv.UpdateOp{
				Key:                 kv.Key{PK: JobPK(job.ID), SK: JobSK},
				ConditionExpression: "#st IN (:queued, :running, :blocked) AND stage_version = :v",
				UpdateExpression:    "SET #st = :failed, current_stage = :terminal, stage_version = :nv, completed_at = :now, updated_at = :now, error_code = :code, error_message = :message, gsi_job_pk = :gpk",
				ExpressionAttributeNames: kv.Names{
					"#st": "status",
				},
				ExpressionAttributeValues: kv.Values{
					":queued":   string(generation.StatusQueued),
					":running":  string(generation.StatusRunning),
					":blocked":  string(generation.StatusBlocked),
					":failed":   string(generation.StatusFailed),
					":terminal": string(generation.StageTerminal),
					":v":        job.StageVersion,
					":nv":       job.StageVersion + 1,
					":now":      now.Format(time.RFC3339Nano),
					":code":     errorCode,
					":message":  errorMessage,
					":gpk":      "TENANT#" + job.TenantID + "#STATUS#" + string(generation.StatusFailed),
				},
			}},
		}, {
			Name: opFlipTerminalMedia,
			Op:   kv.WriteOp{Update: mediaapp.LifecycleFlipOp(job.TenantID, job.MediaID, media.LifecycleFailed, now)},
		}, {
			Name: "workflow.put_terminal",
			Op: kv.WriteOp{Put: &kv.PutOp{
				Item: buildTerminalItem(job, genapp.StageResult{
					NextStage:     genapp.StageTerminal,
					TerminalError: terminalErr,
				}, now),
				ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
			}},
		}},
	}
	failOps := failGenerationOutputOps(*job, terminalErr, now)
	plan.Ops = append(plan.Ops,
		kv.NamedTxOp{Name: opFailGenerationRecord, Op: failOps[0]},
		kv.NamedTxOp{Name: opFailOutputRecord, Op: failOps[1]},
	)
	releaseOps, err := r.terminalQuotaReleaseOps(job)
	if err != nil {
		return err
	}
	plan.Ops = append(plan.Ops, releaseOps...)
	return plan.Execute(ctx, r.KV)
}

func (r *JobRepo) terminalQuotaReleaseOps(job *generation.Job) ([]kv.NamedTxOp, error) {
	if r.QuotaLedger == nil || job.BudgetDate == "" || !genapp.BudgetReleaseAllowed(job.CurrentStage) {
		return nil, nil
	}
	op := r.QuotaLedger.LedgerUpdateReleased(job.TenantID, job.ID, job.BudgetDate, job.BudgetMicroUSD)
	if len(op.Items) != 2 {
		return nil, errors.New("job: release LedgerOp.Items must be [aggregate, ledger] (2 items)")
	}
	return []kv.NamedTxOp{
		{Name: quotaapp.OpAggregateRelease, Op: op.Items[0]},
		{Name: quotaapp.OpLedgerRelease, Op: op.Items[1]},
	}, nil
}

func (r *JobRepo) buildAdvanceJob(job *generation.Job, result genapp.StageResult, now time.Time) *kv.UpdateOp {
	names := kv.Names{
		"#cs": "current_stage",
		"#st": "status",
		"#ua": "updated_at",
	}
	vals := kv.Values{
		":next":    string(result.NextStage),
		":now":     now.Format(time.RFC3339Nano),
		":prev":    string(job.CurrentStage),
		":v":       job.StageVersion,
		":nv":      job.StageVersion + 1,
		":queued":  string(generation.StatusQueued),
		":running": string(generation.StatusRunning),
		":blocked": string(generation.StatusBlocked),
	}
	sets := []string{"#cs = :next", "#ua = :now", "stage_version = :nv"}

	switch result.NextStage {
	case genapp.StageTerminal:
		if result.TerminalError != nil {
			vals[":failed"] = string(generation.StatusFailed)
			vals[":gpk_failed"] = "TENANT#" + job.TenantID + "#STATUS#" + string(generation.StatusFailed)
			sets = append(sets, "#st = :failed", "gsi_job_pk = :gpk_failed")
			vals[":ec"] = result.TerminalError.Code
			vals[":em"] = result.TerminalError.Message
			sets = append(sets, "error_code = :ec", "error_message = :em")
			break
		}
		vals[":complete"] = string(generation.StatusComplete)
		vals[":gpk_complete"] = "TENANT#" + job.TenantID + "#STATUS#" + string(generation.StatusComplete)
		sets = append(sets, "#st = :complete", "gsi_job_pk = :gpk_complete")
		if result.CompletedAt != nil {
			vals[":cat"] = result.CompletedAt.UTC().Format(time.RFC3339Nano)
			sets = append(sets, "completed_at = :cat")
		}
	default:
		vals[":running"] = string(generation.StatusRunning)
		vals[":gpk_running"] = "TENANT#" + job.TenantID + "#STATUS#" + string(generation.StatusRunning)
		sets = append(sets, "#st = :running", "gsi_job_pk = :gpk_running")
	}

	if result.AttemptsDelta != 0 {
		names["#at"] = "attempts"
		sets = append(sets, "#at = if_not_exists(#at, :zero) + :adelta")
		vals[":zero"] = 0
		vals[":adelta"] = result.AttemptsDelta
	}
	if result.BudgetDate != "" {
		vals[":bdate"] = result.BudgetDate
		sets = append(sets, "budget_date = :bdate")
	}
	if result.BudgetMicroUSD != 0 {
		vals[":busd"] = result.BudgetMicroUSD
		sets = append(sets, "budget_micro_usd = :busd")
	}
	if len(result.EncryptedPreparedPrompt) > 0 {
		vals[":eep"] = result.EncryptedPreparedPrompt
		sets = append(sets, "encrypted_prepared_prompt = :eep")
	}
	if result.PreparedPromptHash != "" {
		vals[":pph"] = result.PreparedPromptHash
		sets = append(sets, "prepared_prompt_hash = :pph")
	}
	if result.PromptSpecVersion != "" {
		vals[":psv"] = result.PromptSpecVersion
		sets = append(sets, "prompt_spec_version = :psv")
	}
	if result.GenerationParamsHash != "" {
		vals[":gph"] = result.GenerationParamsHash
		sets = append(sets, "generation_parameters_hash = :gph")
	}
	if result.ProviderRequestID != "" {
		vals[":prid"] = result.ProviderRequestID
		sets = append(sets, "provider_request_id = :prid")
	}
	if result.ProviderJobID != "" {
		vals[":pjid"] = result.ProviderJobID
		sets = append(sets, "provider_job_id = :pjid")
	}
	if result.ResultAssetID != "" {
		vals[":raid"] = result.ResultAssetID
		sets = append(sets, "result_asset_id = :raid")
	}

	return &kv.UpdateOp{
		Key:                       kv.Key{PK: JobPK(job.ID), SK: JobSK},
		ConditionExpression:       "current_stage = :prev AND stage_version = :v AND #st IN (:queued, :running, :blocked)",
		UpdateExpression:          "SET " + strings.Join(sets, ", "),
		ExpressionAttributeNames:  names,
		ExpressionAttributeValues: vals,
	}
}

func (r *JobRepo) buildTerminalMediaUpdate(job *generation.Job, result genapp.StageResult, now time.Time) *kv.UpdateOp {
	terminal := media.LifecycleComplete
	if result.TerminalError != nil {
		terminal = media.LifecycleFailed
	}
	return mediaapp.LifecycleFlipOp(job.TenantID, job.MediaID, terminal, now)
}
