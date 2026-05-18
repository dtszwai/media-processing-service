<script lang="ts">
  import { create } from "@bufbuild/protobuf";
  import {
    CancelJobRequestSchema,
    RetryJobRequestSchema,
    ForceFailJobRequestSchema,
    ReplayOutboxRequestSchema,
  } from "@media-service/api-client/gen/mediaservice/ops/v1/ops_pb.js";
  import MutationButton from "../../lib/MutationButton.svelte";
  import { opsClient } from "../../shared/ops";
  import { isJobTerminal } from "../trace/status";

  let {
    jobId,
    status = "",
    onDone,
  }: {
    jobId: string;
    status?: string;
    onDone?: () => void | Promise<void>;
  } = $props();

  let terminal = $derived(isJobTerminal(status));
  let failed = $derived(status.toUpperCase() === "FAILED");

  async function refresh() {
    await onDone?.();
  }

  async function cancel() {
    await opsClient.cancelJob(create(CancelJobRequestSchema, { jobId, reason: "operator cancel" }));
    await refresh();
  }

  async function retry() {
    await opsClient.retryJob(create(RetryJobRequestSchema, { jobId }));
    await refresh();
  }

  async function forceFail() {
    await opsClient.forceFailJob(create(ForceFailJobRequestSchema, {
      jobId,
      errorCode: "OPERATOR_FORCED_FAIL",
      errorMessage: "manual force-fail",
    }));
    await refresh();
  }

  async function replayOutbox() {
    await opsClient.replayOutbox(create(ReplayOutboxRequestSchema, { jobId }));
    await refresh();
  }
</script>

{#if !terminal}
  <MutationButton
    label="cancel"
    confirmTitle="cancel job"
    confirmBody="Cancel this job. Sets status=CANCELLED with reason=operator cancel."
    target={jobId}
    onConfirm={cancel}
  />
{/if}
{#if failed}
  <MutationButton
    label="retry"
    confirmTitle="retry job"
    confirmBody="Re-queue this job from its last failed stage."
    target={jobId}
    danger={false}
    onConfirm={retry}
  />
{/if}
{#if !terminal}
  <MutationButton
    label="force-fail"
    confirmTitle="force-fail job"
    confirmBody="Mark this job FAILED with error_code=OPERATOR_FORCED_FAIL."
    target={jobId}
    onConfirm={forceFail}
  />
  <MutationButton
    label="replay outbox"
    confirmTitle="replay outbox"
    confirmBody="Republish the OUTBOX#GEN row for this job to its target SNS topic."
    target={jobId}
    danger={false}
    onConfirm={replayOutbox}
  />
{/if}
