/**
 * DLQ queries and mutations using TanStack Query
 */
import { createQuery, createMutation } from "@tanstack/svelte-query";
import { queryClient, queryKeys } from "../../../shared/queries";
import { getDlqStatus, listDlqMessages, replayDlqMessage, deleteDlqMessage, purgeDlq } from "../services";
import { DlqStatusSchema, DlqMessageSchema } from "../../../shared/types";
import type { DlqStatus, DlqMessage } from "../../../shared/types";
import { z } from "zod";

/**
 * Query for DLQ status
 */
export function createDlqStatusQuery(enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.admin.dlq.status(),
    queryFn: async (): Promise<DlqStatus> => {
      const data = await getDlqStatus();
      return DlqStatusSchema.parse(data);
    },
    enabled,
    refetchInterval: 30000, // Refresh every 30 seconds
  }));
}

/**
 * Query for DLQ messages list
 */
export function createDlqMessagesQuery(limit = 10, enabled = true) {
  return createQuery(() => ({
    queryKey: queryKeys.admin.dlq.messages(limit),
    queryFn: async (): Promise<DlqMessage[]> => {
      const data = await listDlqMessages(limit);
      return z.array(DlqMessageSchema).parse(data);
    },
    enabled,
  }));
}

/**
 * Mutation for replaying a DLQ message
 */
export function createReplayDlqMessageMutation() {
  return createMutation(() => ({
    mutationFn: replayDlqMessage,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.dlq.all });
    },
  }));
}

/**
 * Mutation for deleting a DLQ message
 */
export function createDeleteDlqMessageMutation() {
  return createMutation(() => ({
    mutationFn: deleteDlqMessage,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.dlq.all });
    },
  }));
}

/**
 * Mutation for purging all DLQ messages
 */
export function createPurgeDlqMutation() {
  return createMutation(() => ({
    mutationFn: purgeDlq,
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: queryKeys.admin.dlq.all });
    },
  }));
}
