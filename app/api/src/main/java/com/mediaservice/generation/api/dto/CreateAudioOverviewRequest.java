package com.mediaservice.generation.api.dto;

import com.fasterxml.jackson.annotation.JsonProperty;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;
import lombok.Data;
import org.hibernate.validator.constraints.URL;

@Data
public class CreateAudioOverviewRequest {
  @NotBlank
  @Size(min = 1, max = 4000)
  private String topic;
  private String tier;

  @URL(message = "webhook_url must be a valid URL")
  @Pattern(regexp = "^https://.*", message = "webhook_url must use HTTPS")
  @JsonProperty("webhook_url")
  private String webhookUrl;

  /**
   * Optional override for the audio-overview provider used to render this job. Recognised
   * values match the {@code GENERATION_AUDIO_OVERVIEW_PROVIDER} contract (e.g.
   * {@code "simulated"}, {@code "notebooklm"}). Null/blank falls back to the server default
   * configured on the worker.
   */
  @Pattern(regexp = "^[a-zA-Z0-9_-]{1,32}$", message = "provider must be a short identifier")
  private String provider;
}
