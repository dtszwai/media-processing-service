package com.mediaservice.shared.admin.dlq;

import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.responses.ApiResponse;
import io.swagger.v3.oas.annotations.responses.ApiResponses;
import io.swagger.v3.oas.annotations.tags.Tag;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.*;

import java.util.List;
import java.util.Map;

/**
 * Admin controller for Dead Letter Queue management.
 * Provides endpoints to view, replay, and delete failed messages.
 */
@Slf4j
@RestController
@RequestMapping("/admin/dlq")
@RequiredArgsConstructor
@Tag(name = "Admin - DLQ", description = "Dead Letter Queue management endpoints")
public class DlqAdminController {

    private final DlqAdminService dlqAdminService;

    @Operation(summary = "Get DLQ status", description = "Returns queue configuration status and message count")
    @ApiResponses({
            @ApiResponse(responseCode = "200", description = "DLQ status retrieved"),
            @ApiResponse(responseCode = "503", description = "DLQ not configured")
    })
    @GetMapping("/status")
    public ResponseEntity<Map<String, Object>> getStatus() {
        if (!dlqAdminService.isConfigured()) {
            return ResponseEntity.status(503).body(Map.of(
                    "configured", false,
                    "message", "DLQ URL not configured"
            ));
        }
        return ResponseEntity.ok(Map.of(
                "configured", true,
                "approximateMessageCount", dlqAdminService.getApproximateMessageCount()
        ));
    }

    @Operation(summary = "List DLQ messages", description = "Peek at messages without removing them")
    @ApiResponses({
            @ApiResponse(responseCode = "200", description = "Messages retrieved"),
            @ApiResponse(responseCode = "503", description = "DLQ not configured")
    })
    @GetMapping("/messages")
    public ResponseEntity<List<DlqMessage>> listMessages(
            @RequestParam(defaultValue = "10") int limit) {
        if (!dlqAdminService.isConfigured()) {
            return ResponseEntity.status(503).build();
        }
        return ResponseEntity.ok(dlqAdminService.listMessages(limit));
    }

    @Operation(summary = "Get specific DLQ message", description = "Get details of a specific message by ID")
    @ApiResponses({
            @ApiResponse(responseCode = "200", description = "Message found"),
            @ApiResponse(responseCode = "404", description = "Message not found"),
            @ApiResponse(responseCode = "503", description = "DLQ not configured")
    })
    @GetMapping("/messages/{messageId}")
    public ResponseEntity<DlqMessage> getMessage(@PathVariable String messageId) {
        if (!dlqAdminService.isConfigured()) {
            return ResponseEntity.status(503).build();
        }
        return dlqAdminService.getMessage(messageId)
                .map(ResponseEntity::ok)
                .orElse(ResponseEntity.notFound().build());
    }

    @Operation(summary = "Replay DLQ message", description = "Republish message to SNS topic and remove from DLQ")
    @ApiResponses({
            @ApiResponse(responseCode = "200", description = "Message replayed successfully"),
            @ApiResponse(responseCode = "400", description = "Replay failed"),
            @ApiResponse(responseCode = "503", description = "DLQ not configured")
    })
    @PostMapping("/messages/{receiptHandle}/replay")
    public ResponseEntity<Map<String, Object>> replayMessage(@PathVariable String receiptHandle) {
        if (!dlqAdminService.isConfigured()) {
            return ResponseEntity.status(503).body(Map.of(
                    "success", false,
                    "message", "DLQ not configured"
            ));
        }
        boolean success = dlqAdminService.replayMessage(receiptHandle);
        if (success) {
            return ResponseEntity.ok(Map.of(
                    "success", true,
                    "message", "Message replayed and removed from DLQ"
            ));
        }
        return ResponseEntity.badRequest().body(Map.of(
                "success", false,
                "message", "Failed to replay message"
        ));
    }

    @Operation(summary = "Delete DLQ message", description = "Remove message from DLQ without replaying")
    @ApiResponses({
            @ApiResponse(responseCode = "200", description = "Message deleted"),
            @ApiResponse(responseCode = "400", description = "Delete failed"),
            @ApiResponse(responseCode = "503", description = "DLQ not configured")
    })
    @DeleteMapping("/messages/{receiptHandle}")
    public ResponseEntity<Map<String, Object>> deleteMessage(@PathVariable String receiptHandle) {
        if (!dlqAdminService.isConfigured()) {
            return ResponseEntity.status(503).body(Map.of(
                    "success", false,
                    "message", "DLQ not configured"
            ));
        }
        boolean success = dlqAdminService.deleteMessage(receiptHandle);
        if (success) {
            return ResponseEntity.ok(Map.of(
                    "success", true,
                    "message", "Message deleted from DLQ"
            ));
        }
        return ResponseEntity.badRequest().body(Map.of(
                "success", false,
                "message", "Failed to delete message"
        ));
    }

    @Operation(summary = "Purge DLQ", description = "Delete all messages from the DLQ")
    @ApiResponses({
            @ApiResponse(responseCode = "200", description = "Queue purged"),
            @ApiResponse(responseCode = "400", description = "Purge failed"),
            @ApiResponse(responseCode = "503", description = "DLQ not configured")
    })
    @DeleteMapping("/purge")
    public ResponseEntity<Map<String, Object>> purgeQueue() {
        if (!dlqAdminService.isConfigured()) {
            return ResponseEntity.status(503).body(Map.of(
                    "success", false,
                    "message", "DLQ not configured"
            ));
        }
        boolean success = dlqAdminService.purgeQueue();
        if (success) {
            return ResponseEntity.ok(Map.of(
                    "success", true,
                    "message", "DLQ purged successfully"
            ));
        }
        return ResponseEntity.badRequest().body(Map.of(
                "success", false,
                "message", "Failed to purge DLQ"
        ));
    }
}
