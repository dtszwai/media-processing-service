/**
 * DLQ (Dead Letter Queue) Admin API service
 * Handles all DLQ management API calls
 */
import { ADMIN_BASE } from "../../../shared/config/env";
import { handleResponse } from "../../../shared/http";
import { ApiRequestError } from "../../../shared/types";
import type { DlqMessage, DlqStatus } from "../../../shared/types";

const DLQ_BASE = `${ADMIN_BASE}/dlq`;

/**
 * Handle void responses with consistent error handling
 */
async function handleVoidResponse(response: Response, defaultError: string): Promise<void> {
  if (!response.ok) {
    let message = defaultError;
    try {
      const error = await response.json();
      message = error.message || defaultError;
    } catch {
      // Use default error message
    }
    throw new ApiRequestError(message, response.status);
  }
}

/**
 * Get DLQ status (queue configuration and message count)
 */
export async function getDlqStatus(): Promise<DlqStatus> {
  const response = await fetch(`${DLQ_BASE}/status`);
  return handleResponse<DlqStatus>(response);
}

/**
 * List DLQ messages (peek without removing)
 */
export async function listDlqMessages(limit = 10): Promise<DlqMessage[]> {
  const response = await fetch(`${DLQ_BASE}/messages?limit=${limit}`);
  return handleResponse<DlqMessage[]>(response);
}

/**
 * Replay a DLQ message (republish to SNS and remove from DLQ)
 */
export async function replayDlqMessage(receiptHandle: string): Promise<void> {
  const encodedHandle = encodeURIComponent(receiptHandle);
  const response = await fetch(`${DLQ_BASE}/messages/${encodedHandle}/replay`, {
    method: "POST",
  });
  await handleVoidResponse(response, "Replay failed");
}

/**
 * Delete a DLQ message (remove without replaying)
 */
export async function deleteDlqMessage(receiptHandle: string): Promise<void> {
  const encodedHandle = encodeURIComponent(receiptHandle);
  const response = await fetch(`${DLQ_BASE}/messages/${encodedHandle}`, {
    method: "DELETE",
  });
  await handleVoidResponse(response, "Delete failed");
}

/**
 * Purge all messages from DLQ
 */
export async function purgeDlq(): Promise<void> {
  const response = await fetch(`${DLQ_BASE}/purge`, {
    method: "DELETE",
  });
  await handleVoidResponse(response, "Purge failed");
}
