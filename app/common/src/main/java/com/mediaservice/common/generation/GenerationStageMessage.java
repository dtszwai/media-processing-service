package com.mediaservice.common.generation;

import com.fasterxml.jackson.annotation.JsonProperty;
import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class GenerationStageMessage {

  /**
   * Decode either a raw stage-message payload or an SNS-wrapped envelope. SQS-from-SNS bodies
   * include the topic envelope ({@code {"Type":"Notification","Message":"…"}}); raw publishes
   * (RawMessageDelivery=true or direct test injection) skip it. Both poller paths share this
   * unwrap so the SNS-envelope detection lives in one place.
   */
  public static GenerationStageMessage fromSqsBody(String body, ObjectMapper mapper) throws java.io.IOException {
    JsonNode node = mapper.readTree(body);
    if (node.has("Message")) {
      node = mapper.readTree(node.get("Message").asText());
    }
    return mapper.treeToValue(node, GenerationStageMessage.class);
  }

  @JsonProperty("job_id")
  private String jobId;

  private GenerationStage stage;
  private int attempt;

  /**
   * Poll counter for async-still-running re-publishes. Separate from {@link #attempt} so a
   * long-running async job that spawns hundreds of polls does not exhaust the stage retry budget.
   */
  @JsonProperty("poll_count")
  private int pollCount;

  /**
   * Job tier ({@code "free"} or {@code "paid"}). Carried on the message so SNS subscription
   * filter policies can route to per-tier SQS queues without a publisher-side DynamoDB read.
   * Missing or unknown tiers fall through to the free-tier subscription.
   */
  private String tier;
}
