package com.mediaservice.generation.api.dto;

import com.fasterxml.jackson.annotation.JsonProperty;
import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Size;
import lombok.Data;
import org.hibernate.validator.constraints.URL;

@Data
public class CreateGenerationRequest {
  @NotBlank
  @Size(min = 1, max = 4000)
  private String prompt;

  private String model;
  private String resolution;
  private String tier;
  private Long seed;

  @URL(message = "webhook_url must be a valid URL")
  @Pattern(regexp = "^https://.*", message = "webhook_url must use HTTPS")
  @JsonProperty("webhook_url")
  @Schema(description = "Optional HTTPS URL to receive webhook notification when generation completes")
  private String webhookUrl;
}
