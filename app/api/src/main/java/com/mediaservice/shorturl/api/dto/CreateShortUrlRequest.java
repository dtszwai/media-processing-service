package com.mediaservice.shorturl.api.dto;

import io.swagger.v3.oas.annotations.media.Schema;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Pattern;
import java.time.Instant;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class CreateShortUrlRequest {
  @NotBlank(message = "mediaId is required")
  private String mediaId;

  @NotBlank(message = "assetId is required")
  private String assetId;

  @Pattern(regexp = "^[a-z0-9][a-z0-9_-]*$", message = "alias may contain lowercase letters, numbers, '-' or '_'")
  @Schema(description = "Optional custom alias for the short URL (lowercase letters, numbers, '-' or '_')")
  private String alias;

  @Schema(description = "Optional expiration timestamp in ISO-8601 format", example = "2026-02-09T00:00:00Z")
  private Instant expiresAt;

  @Schema(description = "Optional label to help manage short URLs")
  private String label;
}
