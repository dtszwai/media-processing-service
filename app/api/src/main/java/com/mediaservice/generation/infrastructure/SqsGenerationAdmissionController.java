package com.mediaservice.generation.infrastructure;

import com.mediaservice.providers.generation.core.AdmissionVerdict;
import com.mediaservice.providers.generation.core.GenerationAdmissionController;
import com.mediaservice.providers.generation.core.GenerationSubmission;
import java.util.Map;
import lombok.extern.slf4j.Slf4j;
import software.amazon.awssdk.services.sqs.SqsClient;
import software.amazon.awssdk.services.sqs.model.GetQueueAttributesRequest;
import software.amazon.awssdk.services.sqs.model.QueueAttributeName;

/**
 * Resource-pressure admission controller that consults SQS queue depth.
 *
 * <p><strong>Do not use this controller standalone.</strong> It only enforces queue-depth
 * backpressure and intentionally knows nothing about tier quotas, outstanding-job limits,
 * abuse signals, or balance checks. It MUST be wrapped by
 * {@link RedisGenerationAdmissionController}, which composes these checks into the full
 * admission pipeline. Direct callers will silently bypass quota tracking and budget controls.
 */
@Slf4j
public class SqsGenerationAdmissionController implements GenerationAdmissionController {
  private final SqsClient sqsClient;
  private final String queueUrl;
  private final int maxQueueDepth;
  private final int delayedThresholdPct;
  private final int degradedThresholdPct;
  private final int retryAfterSeconds;
  private final boolean failOpen;

  public SqsGenerationAdmissionController(SqsClient sqsClient, String queueUrl, int maxQueueDepth,
      int delayedThresholdPct, int degradedThresholdPct, int retryAfterSeconds, boolean failOpen) {
    this.sqsClient = sqsClient;
    this.queueUrl = queueUrl;
    this.maxQueueDepth = maxQueueDepth;
    this.delayedThresholdPct = delayedThresholdPct;
    this.degradedThresholdPct = degradedThresholdPct;
    this.retryAfterSeconds = retryAfterSeconds;
    this.failOpen = failOpen;
  }

  @Override
  public AdmissionVerdict evaluate(GenerationSubmission submission) {
    if (queueUrl == null || queueUrl.isBlank() || maxQueueDepth <= 0) {
      return AdmissionVerdict.allow();
    }
    try {
      var response = sqsClient.getQueueAttributes(GetQueueAttributesRequest.builder()
          .queueUrl(queueUrl)
          .attributeNames(
              QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES,
              QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES_NOT_VISIBLE)
          .build());
      int visible = parse(response.attributes().get(QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES));
      int inFlight = parse(response.attributes().get(QueueAttributeName.APPROXIMATE_NUMBER_OF_MESSAGES_NOT_VISIBLE));
      int depth = visible + inFlight;
      if (depth >= maxQueueDepth) {
        return AdmissionVerdict.reject("ADMISSION_BACKPRESSURE",
            "Generation queue is temporarily saturated; retry later", retryAfterSeconds);
      }
      int loadPct = (int) Math.ceil((depth * 100.0) / maxQueueDepth);
      Map<String, String> metadata = Map.of(
          "queue_depth", String.valueOf(depth),
          "queue_visible", String.valueOf(visible),
          "queue_in_flight", String.valueOf(inFlight),
          "queue_load_pct", String.valueOf(loadPct));
      if (loadPct >= degradedThresholdPct) {
        return AdmissionVerdict.degraded("ADMISSION_DEGRADED",
            "Generation accepted with degraded simulator quality due to queue pressure",
            retryAfterSeconds,
            metadata);
      }
      if (loadPct >= delayedThresholdPct) {
        return AdmissionVerdict.acceptedDelayed("ADMISSION_ACCEPTED_DELAYED",
            "Generation accepted with elevated wait time due to queue pressure",
            retryAfterSeconds,
            metadata);
      }
      return AdmissionVerdict.allow();
    } catch (RuntimeException e) {
      log.warn("Failed to evaluate generation queue depth for admission: {}", e.getMessage());
      if (failOpen) {
        return AdmissionVerdict.allow();
      }
      return AdmissionVerdict.reject("ADMISSION_CHECK_UNAVAILABLE",
          "Generation admission control is temporarily unavailable", retryAfterSeconds);
    }
  }

  private int parse(String value) {
    if (value == null || value.isBlank()) {
      return 0;
    }
    return Integer.parseInt(value);
  }
}
