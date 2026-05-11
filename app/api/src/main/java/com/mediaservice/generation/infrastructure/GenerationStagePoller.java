package com.mediaservice.generation.infrastructure;

import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.generation.GenerationStageMessage;
import com.mediaservice.providers.generation.core.GenerationWorkflow;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.atomic.AtomicBoolean;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.autoconfigure.condition.ConditionalOnProperty;
import org.springframework.context.SmartLifecycle;
import org.springframework.stereotype.Component;
import software.amazon.awssdk.services.sqs.SqsClient;
import software.amazon.awssdk.services.sqs.model.DeleteMessageRequest;
import software.amazon.awssdk.services.sqs.model.Message;
import software.amazon.awssdk.services.sqs.model.ReceiveMessageRequest;

/**
 * In-process SQS consumer for generation stage messages.
 *
 * <p>LocalStack community edition does not support container-image Lambdas
 * (it errors with {@code Container images are a Pro feature}), so the
 * {@link com.mediaservice.lambda.GenerationWorkerHandler} cannot host the
 * Python-based NotebookLM bridge in local mode. This poller bypasses Lambda
 * entirely by running the same workflow loop inside the API JVM — the API
 * container already ships Python and {@code notebooklm-py} for the bridge.
 *
 * <p>The terraform module is wired to skip the generation-worker SQS event
 * source mappings when {@code local_stage_poller_enabled = true}, so the
 * poller is the sole consumer and there is no double-processing. Production
 * deployments leave this bean disabled and rely on the Lambda event-source-
 * mappings as before.
 */
@Component
@ConditionalOnProperty(name = "media.generation.local-stage-poller.enabled", havingValue = "true")
public class GenerationStagePoller implements SmartLifecycle {
  private static final Logger log = LoggerFactory.getLogger(GenerationStagePoller.class);
  private static final ObjectMapper MAPPER = new ObjectMapper().findAndRegisterModules();

  private final SqsClient sqsClient;
  private final GenerationWorkflow workflow;
  private final List<String> queueUrls;
  private final int maxMessagesPerPoll;
  private final int waitTimeSeconds;
  private final int visibilityTimeoutSeconds;

  private final AtomicBoolean running = new AtomicBoolean(false);
  private Thread workerThread;

  public GenerationStagePoller(SqsClient sqsClient, GenerationWorkflow workflow,
      @Value("${media.generation.queue-url:}") String freeQueueUrl,
      @Value("${media.generation.paid-queue-url:}") String paidQueueUrl,
      @Value("${media.generation.local-stage-poller.max-messages:5}") int maxMessages,
      @Value("${media.generation.local-stage-poller.wait-seconds:10}") int waitSeconds,
      @Value("${media.generation.local-stage-poller.visibility-seconds:300}") int visibilitySeconds) {
    this.sqsClient = sqsClient;
    this.workflow = workflow;
    this.queueUrls = new ArrayList<>();
    if (freeQueueUrl != null && !freeQueueUrl.isBlank()) {
      this.queueUrls.add(freeQueueUrl);
    }
    if (paidQueueUrl != null && !paidQueueUrl.isBlank()) {
      this.queueUrls.add(paidQueueUrl);
    }
    this.maxMessagesPerPoll = maxMessages;
    this.waitTimeSeconds = waitSeconds;
    this.visibilityTimeoutSeconds = visibilitySeconds;
  }

  @Override
  public void start() {
    if (queueUrls.isEmpty()) {
      log.warn("GenerationStagePoller enabled but no queue URLs configured; staying idle");
      return;
    }
    if (!running.compareAndSet(false, true)) {
      return;
    }
    workerThread = new Thread(this::loop, "generation-stage-poller");
    workerThread.setDaemon(true);
    workerThread.start();
    log.info("GenerationStagePoller started; queues={}", queueUrls);
  }

  @Override
  public void stop() {
    if (!running.compareAndSet(true, false)) {
      return;
    }
    if (workerThread != null) {
      workerThread.interrupt();
      try {
        workerThread.join(5_000);
      } catch (InterruptedException ie) {
        Thread.currentThread().interrupt();
      }
    }
    log.info("GenerationStagePoller stopped");
  }

  @Override
  public boolean isRunning() {
    return running.get();
  }

  private void loop() {
    int consecutiveFailures = 0;
    while (running.get()) {
      boolean idle = true;
      boolean anyFailure = false;
      for (String queueUrl : queueUrls) {
        if (!running.get()) {
          return;
        }
        try {
          int processed = drainOnce(queueUrl);
          if (processed > 0) {
            idle = false;
          }
        } catch (Exception e) {
          if (Thread.currentThread().isInterrupted()) {
            return;
          }
          anyFailure = true;
          log.warn("GenerationStagePoller error on {}: {}", queueUrl, e.getMessage());
        }
      }
      if (anyFailure) {
        consecutiveFailures = Math.min(consecutiveFailures + 1, 5);
        long backoffMs = Math.min(30_000L, 1_000L << (consecutiveFailures - 1));
        if (!sleepQuietly(backoffMs)) {
          return;
        }
      } else {
        consecutiveFailures = 0;
        // No extra idle sleep: SQS long-poll (waitTimeSeconds) already paces the loop when no
        // messages arrive. A 200ms top-up burned ~5 redundant ReceiveMessage calls/sec idle.
      }
    }
  }

  private boolean sleepQuietly(long millis) {
    try {
      Thread.sleep(millis);
      return true;
    } catch (InterruptedException ie) {
      Thread.currentThread().interrupt();
      return false;
    }
  }

  private int drainOnce(String queueUrl) {
    var resp = sqsClient.receiveMessage(ReceiveMessageRequest.builder()
        .queueUrl(queueUrl)
        .maxNumberOfMessages(maxMessagesPerPoll)
        .waitTimeSeconds(waitTimeSeconds)
        .visibilityTimeout(visibilityTimeoutSeconds)
        .build());
    int processed = 0;
    for (Message m : resp.messages()) {
      try {
        GenerationStageMessage payload = parseRawMessage(m.body());
        log.info("Stage poller processing jobId={} stage={} attempt={}",
            payload.getJobId(), payload.getStage(), payload.getAttempt());
        workflow.processStage(payload);
        sqsClient.deleteMessage(DeleteMessageRequest.builder()
            .queueUrl(queueUrl)
            .receiptHandle(m.receiptHandle())
            .build());
        processed++;
      } catch (Exception e) {
        // Leave the message in flight; SQS visibility timeout returns it to the
        // queue, mirroring Lambda's at-least-once delivery semantics. The DLQ
        // catches poison messages once redrive policy exhausts attempts.
        log.error("Stage poller failed for messageId={}: {}", m.messageId(), e.getMessage(), e);
      }
    }
    return processed;
  }

  private GenerationStageMessage parseRawMessage(String body) throws Exception {
    return GenerationStageMessage.fromSqsBody(body, MAPPER);
  }
}
