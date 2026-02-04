package com.mediaservice.shared.admin.dlq;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.services.sns.SnsClient;
import software.amazon.awssdk.services.sqs.SqsClient;
import software.amazon.awssdk.services.sqs.model.*;

import java.time.Instant;
import java.util.List;
import java.util.Map;
import java.util.Optional;

/**
 * Service for managing Dead Letter Queue messages.
 * Provides functionality to list, view, replay, and delete DLQ messages.
 */
@Slf4j
@Service
public class DlqAdminService {

    private final SqsClient sqsClient;
    private final SnsClient snsClient;
    private final ObjectMapper objectMapper;
    private final String dlqUrl;
    private final String snsTopicArn;

    public DlqAdminService(
            SqsClient sqsClient,
            SnsClient snsClient,
            ObjectMapper objectMapper,
            @Value("${aws.sqs.dlq-url:}") String dlqUrl,
            @Value("${aws.sns.topic-arn:}") String snsTopicArn) {
        this.sqsClient = sqsClient;
        this.snsClient = snsClient;
        this.objectMapper = objectMapper;
        this.dlqUrl = dlqUrl;
        this.snsTopicArn = snsTopicArn;
    }

    /**
     * Check if DLQ service is configured.
     */
    public boolean isConfigured() {
        return dlqUrl != null && !dlqUrl.isEmpty();
    }

    /**
     * Get approximate number of messages in DLQ.
     */
    public int getApproximateMessageCount() {
        if (!isConfigured()) {
            return 0;
        }
        try {
            var response = sqsClient.getQueueAttributes(GetQueueAttributesRequest.builder()
                    .queueUrl(dlqUrl)
                    .attributeNames(QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES)
                    .build());
            String count = response.attributes().get(QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES);
            return count != null ? Integer.parseInt(count) : 0;
        } catch (Exception e) {
            log.error("Failed to get DLQ message count: {}", e.getMessage());
            return 0;
        }
    }

    /**
     * List messages from the DLQ (non-destructive peek).
     *
     * @param maxMessages maximum number of messages to retrieve (1-10)
     * @return list of DLQ messages
     */
    public List<DlqMessage> listMessages(int maxMessages) {
        if (!isConfigured()) {
            return List.of();
        }
        int limit = Math.min(Math.max(maxMessages, 1), 10);
        try {
            var response = sqsClient.receiveMessage(ReceiveMessageRequest.builder()
                    .queueUrl(dlqUrl)
                    .maxNumberOfMessages(limit)
                    .visibilityTimeout(0) // Don't hide messages (peek mode)
                    .attributeNames(QueueAttributeName.ALL)
                    .messageAttributeNames("All")
                    .build());
            return response.messages().stream()
                    .map(this::toDto)
                    .toList();
        } catch (Exception e) {
            log.error("Failed to list DLQ messages: {}", e.getMessage());
            return List.of();
        }
    }

    /**
     * Get a specific message by ID.
     * Note: SQS doesn't support direct get by ID, so this peeks all and filters.
     */
    public Optional<DlqMessage> getMessage(String messageId) {
        return listMessages(10).stream()
                .filter(m -> m.getMessageId().equals(messageId))
                .findFirst();
    }

    /**
     * Replay a message by republishing to SNS topic.
     *
     * @param receiptHandle the receipt handle of the message to replay
     * @return true if replay was successful
     */
    public boolean replayMessage(String receiptHandle) {
        if (!isConfigured() || snsTopicArn == null || snsTopicArn.isEmpty()) {
            log.warn("Cannot replay message: DLQ or SNS not configured");
            return false;
        }
        try {
            // First, receive the message to get its body
            var receiveResponse = sqsClient.receiveMessage(ReceiveMessageRequest.builder()
                    .queueUrl(dlqUrl)
                    .maxNumberOfMessages(10)
                    .visibilityTimeout(30)
                    .build());

            var targetMessage = receiveResponse.messages().stream()
                    .filter(m -> m.receiptHandle().equals(receiptHandle))
                    .findFirst();

            if (targetMessage.isEmpty()) {
                log.warn("Message not found with receipt handle: {}", receiptHandle);
                return false;
            }

            var message = targetMessage.get();
            String body = message.body();

            // Parse SNS envelope if present
            String actualPayload = extractPayloadFromSnsEnvelope(body);

            // Republish to SNS
            snsClient.publish(builder -> builder
                    .topicArn(snsTopicArn)
                    .message(actualPayload)
                    .build());

            log.info("Replayed message {} to SNS", message.messageId());

            // Delete from DLQ after successful replay
            sqsClient.deleteMessage(DeleteMessageRequest.builder()
                    .queueUrl(dlqUrl)
                    .receiptHandle(receiptHandle)
                    .build());

            log.info("Deleted replayed message from DLQ: {}", message.messageId());
            return true;
        } catch (Exception e) {
            log.error("Failed to replay message: {}", e.getMessage(), e);
            return false;
        }
    }

    /**
     * Delete a message from the DLQ without replaying.
     *
     * @param receiptHandle the receipt handle of the message to delete
     * @return true if deletion was successful
     */
    public boolean deleteMessage(String receiptHandle) {
        if (!isConfigured()) {
            return false;
        }
        try {
            sqsClient.deleteMessage(DeleteMessageRequest.builder()
                    .queueUrl(dlqUrl)
                    .receiptHandle(receiptHandle)
                    .build());
            log.info("Deleted message from DLQ");
            return true;
        } catch (Exception e) {
            log.error("Failed to delete DLQ message: {}", e.getMessage());
            return false;
        }
    }

    /**
     * Purge all messages from the DLQ.
     *
     * @return true if purge was successful
     */
    public boolean purgeQueue() {
        if (!isConfigured()) {
            return false;
        }
        try {
            sqsClient.purgeQueue(PurgeQueueRequest.builder()
                    .queueUrl(dlqUrl)
                    .build());
            log.info("Purged DLQ");
            return true;
        } catch (Exception e) {
            log.error("Failed to purge DLQ: {}", e.getMessage());
            return false;
        }
    }

    private DlqMessage toDto(Message message) {
        var attributes = message.attributes();
        long sentTimestampMs = 0;
        int receiveCount = 0;

        if (attributes.containsKey(MessageSystemAttributeName.SENT_TIMESTAMP)) {
            sentTimestampMs = Long.parseLong(attributes.get(MessageSystemAttributeName.SENT_TIMESTAMP));
        }
        if (attributes.containsKey(MessageSystemAttributeName.APPROXIMATE_RECEIVE_COUNT)) {
            receiveCount = Integer.parseInt(attributes.get(MessageSystemAttributeName.APPROXIMATE_RECEIVE_COUNT));
        }

        return DlqMessage.builder()
                .messageId(message.messageId())
                .receiptHandle(message.receiptHandle())
                .body(message.body())
                .attributes(Map.of(
                        "sentTimestamp", String.valueOf(sentTimestampMs),
                        "approximateReceiveCount", String.valueOf(receiveCount)
                ))
                .sentTimestamp(Instant.ofEpochMilli(sentTimestampMs))
                .approximateReceiveCount(receiveCount)
                .build();
    }

    /**
     * Extract the actual message payload from an SNS envelope.
     * SNS wraps messages in an envelope when delivering to SQS.
     *
     * @param body the raw message body (potentially SNS-wrapped)
     * @return the extracted message payload, or original body if not wrapped
     */
    private String extractPayloadFromSnsEnvelope(String body) {
        try {
            JsonNode envelope = objectMapper.readTree(body);

            // Check if this is an SNS envelope (has Type and Message fields)
            if (envelope.has("Type") && envelope.has("Message")) {
                String messageType = envelope.get("Type").asText();
                if ("Notification".equals(messageType)) {
                    return envelope.get("Message").asText();
                }
            }
        } catch (Exception e) {
            log.debug("Could not parse as SNS envelope, using original body: {}", e.getMessage());
        }
        return body;
    }
}
