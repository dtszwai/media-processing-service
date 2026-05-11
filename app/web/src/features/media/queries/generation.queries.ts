/**
 * Generation queries and mutations using TanStack Query
 */
import { createMutation, createQuery } from "@tanstack/svelte-query";
import { queryClient, queryKeys } from "../../../shared/queries";
import {
  createAudioOverview,
  createGeneration,
  getGeneration,
  getGenerationResult,
} from "../services";
import {
  GenerationResponseSchema,
  GenerationResultResponseSchema,
  GenerationStatusResponseSchema,
} from "../../../shared/types";
import type {
  CreateAudioOverviewRequest,
  CreateGenerationRequest,
  GenerationResultResponse,
  GenerationStatusResponse,
} from "../../../shared/types";

export function createGenerationMutation() {
  return createMutation(() => ({
    mutationFn: async ({ request, idempotencyKey }: { request: CreateGenerationRequest; idempotencyKey?: string }) => {
      const data = await createGeneration(request, idempotencyKey);
      return GenerationResponseSchema.parse(data);
    },
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.generation.detail(result.job_id) });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

export function createAudioOverviewMutation() {
  return createMutation(() => ({
    mutationFn: async ({
      request,
      idempotencyKey,
    }: {
      request: CreateAudioOverviewRequest;
      idempotencyKey?: string;
    }) => {
      const data = await createAudioOverview(request, idempotencyKey);
      return GenerationResponseSchema.parse(data);
    },
    onSuccess: (result) => {
      queryClient.invalidateQueries({ queryKey: queryKeys.generation.detail(result.job_id) });
      queryClient.invalidateQueries({ queryKey: queryKeys.media.all });
    },
  }));
}

export function createGenerationQuery(jobId: string, enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.generation.detail(jobId),
    queryFn: async (): Promise<GenerationStatusResponse> => {
      const data = await getGeneration(jobId);
      return GenerationStatusResponseSchema.parse(data);
    },
    enabled,
    refetchInterval: (query) => {
      const status = query.state.data?.status;
      return status === "QUEUED" || status === "RUNNING" ? 2000 : false;
    },
  }));
}

export function createGenerationResultQuery(
  jobId: () => string | undefined,
  status: () => string | undefined,
) {
  return createQuery(() => ({
    queryKey: queryKeys.generation.result(jobId() ?? ""),
    queryFn: async (): Promise<GenerationResultResponse> => {
      const data = await getGenerationResult(jobId()!);
      return GenerationResultResponseSchema.parse(data);
    },
    enabled: !!jobId() && status() === "COMPLETE",
  }));
}
