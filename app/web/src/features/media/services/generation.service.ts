/**
 * Generation API service
 */
import { AUDIO_OVERVIEW_BASE, GENERATION_BASE } from "../../../shared/config/env";
import { authenticatedFetch, handleResponse } from "../../../shared/http";
import type {
  CreateAudioOverviewRequest,
  CreateGenerationRequest,
  GenerationResponse,
  GenerationResultResponse,
} from "../../../shared/types";

function idempotencyHeaders(idempotencyKey?: string): Record<string, string> {
  return idempotencyKey
    ? { "Content-Type": "application/json", "Idempotency-Key": idempotencyKey }
    : { "Content-Type": "application/json" };
}

export async function createGeneration(
  request: CreateGenerationRequest,
  idempotencyKey?: string,
): Promise<GenerationResponse> {
  const response = await authenticatedFetch(GENERATION_BASE, {
    method: "POST",
    headers: idempotencyHeaders(idempotencyKey),
    body: JSON.stringify(request),
  });
  return handleResponse<GenerationResponse>(response);
}

export async function createAudioOverview(
  request: CreateAudioOverviewRequest,
  idempotencyKey?: string,
): Promise<GenerationResponse> {
  const response = await authenticatedFetch(AUDIO_OVERVIEW_BASE, {
    method: "POST",
    headers: idempotencyHeaders(idempotencyKey),
    body: JSON.stringify(request),
  });
  return handleResponse<GenerationResponse>(response);
}

export async function getGeneration(jobId: string): Promise<GenerationResponse> {
  const response = await authenticatedFetch(`${GENERATION_BASE}/${jobId}`);
  if (response.status === 404) {
    throw new Error("NOT_FOUND");
  }
  return handleResponse<GenerationResponse>(response);
}

export async function getGenerationResult(jobId: string): Promise<GenerationResultResponse> {
  const response = await authenticatedFetch(`${GENERATION_BASE}/${jobId}/result`);
  if (response.status === 404) {
    throw new Error("NOT_FOUND");
  }
  return handleResponse<GenerationResultResponse>(response);
}
