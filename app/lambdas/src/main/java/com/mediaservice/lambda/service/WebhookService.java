package com.mediaservice.lambda.service;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import com.mediaservice.common.model.Media;
import com.mediaservice.lambda.config.LambdaConfig;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

import javax.crypto.Mac;
import javax.crypto.spec.SecretKeySpec;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.time.Instant;
import java.util.Base64;
import java.util.Map;

/**
 * Service for sending webhook notifications when media processing completes.
 * Supports HMAC-SHA256 signature for request verification.
 */
public class WebhookService {
    private static final Logger logger = LoggerFactory.getLogger(WebhookService.class);
    private static final String HMAC_ALGORITHM = "HmacSHA256";
    private static final Duration REQUEST_TIMEOUT = Duration.ofSeconds(30);
    private static final int MAX_RETRIES = 3;
    private static final Duration RETRY_DELAY = Duration.ofSeconds(1);

    private final HttpClient httpClient;
    private final ObjectMapper objectMapper;
    private final String webhookSecret;

    public WebhookService() {
        this.httpClient = HttpClient.newBuilder()
                .connectTimeout(Duration.ofSeconds(10))
                .build();
        this.objectMapper = new ObjectMapper()
                .registerModule(new JavaTimeModule())
                .configure(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS, false);
        this.webhookSecret = LambdaConfig.getInstance().getWebhookSecret();
    }

    // Test constructor
    WebhookService(HttpClient httpClient, String webhookSecret) {
        this.httpClient = httpClient;
        this.objectMapper = new ObjectMapper()
                .registerModule(new JavaTimeModule())
                .configure(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS, false);
        this.webhookSecret = webhookSecret;
    }

    /**
     * Send webhook notification for media processing completion.
     *
     * @param media      The media entity with processing result
     * @param webhookUrl The URL to send the notification to
     * @return true if notification was sent successfully
     */
    public boolean sendCompletionNotification(Media media, String webhookUrl) {
        if (webhookUrl == null || webhookUrl.isEmpty()) {
            logger.debug("No webhook URL configured for media: {}", media.getMediaId());
            return false;
        }

        // Validate URL is HTTPS
        if (!webhookUrl.startsWith("https://")) {
            logger.warn("Webhook URL must use HTTPS: {}", webhookUrl);
            return false;
        }

        try {
            WebhookPayload payload = buildPayload(media);
            String jsonPayload = objectMapper.writeValueAsString(payload);
            String signature = generateSignature(jsonPayload);
            String timestamp = String.valueOf(Instant.now().getEpochSecond());

            return sendWithRetry(webhookUrl, jsonPayload, signature, timestamp);
        } catch (Exception e) {
            logger.error("Failed to send webhook for media {}: {}", media.getMediaId(), e.getMessage(), e);
            return false;
        }
    }

    private WebhookPayload buildPayload(Media media) {
            return new WebhookPayload(
                    "media.processing.complete",
                    media.getMediaId(),
                    media.getStatus() != null ? media.getStatus().name() : null,
                    media.getName(),
                    media.getMimetype(),
                    media.getOriginalAssetId(),
                    Instant.now()
            );
    }

    private String generateSignature(String payload) {
        if (webhookSecret == null || webhookSecret.isEmpty()) {
            logger.warn("Webhook secret not configured, sending unsigned request");
            return "";
        }

        try {
            Mac mac = Mac.getInstance(HMAC_ALGORITHM);
            SecretKeySpec secretKey = new SecretKeySpec(
                    webhookSecret.getBytes(StandardCharsets.UTF_8),
                    HMAC_ALGORITHM);
            mac.init(secretKey);
            byte[] hmacBytes = mac.doFinal(payload.getBytes(StandardCharsets.UTF_8));
            return Base64.getEncoder().encodeToString(hmacBytes);
        } catch (Exception e) {
            logger.error("Failed to generate HMAC signature: {}", e.getMessage());
            return "";
        }
    }

    private boolean sendWithRetry(String url, String payload, String signature, String timestamp) {
        for (int attempt = 1; attempt <= MAX_RETRIES; attempt++) {
            try {
                HttpRequest.Builder requestBuilder = HttpRequest.newBuilder()
                        .uri(URI.create(url))
                        .timeout(REQUEST_TIMEOUT)
                        .header("Content-Type", "application/json")
                        .header("X-Webhook-Timestamp", timestamp)
                        .POST(HttpRequest.BodyPublishers.ofString(payload));

                if (!signature.isEmpty()) {
                    requestBuilder.header("X-Webhook-Signature", signature);
                }

                HttpResponse<String> response = httpClient.send(
                        requestBuilder.build(),
                        HttpResponse.BodyHandlers.ofString());

                int statusCode = response.statusCode();
                if (statusCode >= 200 && statusCode < 300) {
                    logger.info("Webhook sent successfully to {} (attempt {})", url, attempt);
                    return true;
                }

                // Don't retry client errors (4xx)
                if (statusCode >= 400 && statusCode < 500) {
                    logger.warn("Webhook rejected by server: {} - {} (no retry)", statusCode, response.body());
                    return false;
                }

                logger.warn("Webhook failed with status {} (attempt {}): {}", statusCode, attempt, response.body());
            } catch (Exception e) {
                logger.warn("Webhook request failed (attempt {}): {}", attempt, e.getMessage());
            }

            // Wait before retry
            if (attempt < MAX_RETRIES) {
                try {
                    Thread.sleep(RETRY_DELAY.toMillis() * attempt); // Exponential backoff
                } catch (InterruptedException ie) {
                    Thread.currentThread().interrupt();
                    return false;
                }
            }
        }

        logger.error("Webhook delivery failed after {} attempts to {}", MAX_RETRIES, url);
        return false;
    }

    /**
     * Webhook payload sent to the configured URL.
     */
    public record WebhookPayload(
            String event,
            String mediaId,
            String status,
            String fileName,
            String mimeType,
            String originalAssetId,
            Instant timestamp
    ) {}
}
