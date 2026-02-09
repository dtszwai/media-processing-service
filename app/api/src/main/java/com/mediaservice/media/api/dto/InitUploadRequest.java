package com.mediaservice.media.api.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import com.mediaservice.common.model.MediaType;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Positive;
import org.hibernate.validator.constraints.URL;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class InitUploadRequest {
  @NotBlank(message = "fileName is required")
  private String fileName;

  @Positive(message = "fileSize must be positive")
  private long fileSize;

  @NotBlank(message = "contentType is required")
  private String contentType;

  private MediaType mediaType;

  @URL(message = "webhookUrl must be a valid URL")
  @Pattern(regexp = "^https://.*", message = "webhookUrl must use HTTPS")
  @Schema(description = "Optional HTTPS URL to receive webhook notification when processing completes")
  private String webhookUrl;
}
