package com.mediaservice.common.generation;

/**
 * Canonical error codes raised by the generation pipeline. Stringified via {@link #name()} for
 * wire/storage compatibility — DynamoDB rows, SQS retry classification, and OpenTelemetry spans
 * all key off the enum's name. Adding a new code anywhere in the pipeline starts here.
 *
 * <p>Admission-layer verdict codes live separately on the verdict record, not here — those are
 * routing decisions, not pipeline faults.
 */
public enum GenerationErrorCode {
  AI_DISCLOSURE_MISSING,
  BUDGET_EXCEEDED,
  CHECKSUM_FAILED,
  HASH_FAILED,
  INTERRUPTED,
  INVALID_JOB_SPEC,
  JOB_NOT_FOUND,
  MEDIA_NOT_FOUND,
  MODERATION_BLOCKED,
  MODERATION_REJECTED,
  NOTEBOOKLM_AUTH_EXPIRED,
  NOTEBOOKLM_BRIDGE_FAILED,
  NOTEBOOKLM_EMPTY_ARTIFACT,
  NOTEBOOKLM_IO_FAILED,
  NOTEBOOKLM_RPC_FAILED,
  OPENAI_ARTIFACT_FETCH_FAILED,
  OPENAI_CLIENT_ERROR,
  OPENAI_EMPTY_RESPONSE,
  OPENAI_FORBIDDEN,
  OPENAI_RATE_LIMITED,
  OPENAI_REQUEST_FAILED,
  OPENAI_SECRET_READ_FAILED,
  OPENAI_SERVER_ERROR,
  OPENAI_UNAUTHORIZED,
  OUTPUT_BLOCKED,
  OUTPUT_SAFETY_MISSING,
  POLL_EXHAUSTED,
  POST_REWRITE_BLOCKED,
  PROVIDER_JOB_FAILED,
  PROVIDER_JOB_MISSING,
  PROVIDER_JOB_NOT_FOUND,
  PROVIDER_TIMEOUT,
  SIMULATED_PROVIDER_FAILURE,
  SIMULATOR_PAUSED,
  SIMULATOR_RENDER_FAILED,
  STAGE_FAILED,
  UNKNOWN_OUTCOME_UNRECOVERABLE,
  UNSUPPORTED_PROVIDER,
  WATERMARK_MISSING,
  WATERMARK_STAMP_FAILED,
}
