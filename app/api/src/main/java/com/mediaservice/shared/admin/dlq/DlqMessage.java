package com.mediaservice.shared.admin.dlq;

import lombok.Builder;
import lombok.Data;

import java.time.Instant;
import java.util.Map;

/**
 * DTO representing a message in the DLQ.
 */
@Data
@Builder
public class DlqMessage {
    private String messageId;
    private String receiptHandle;
    private String body;
    private Map<String, String> attributes;
    private Instant sentTimestamp;
    private int approximateReceiveCount;
}
