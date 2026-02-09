package com.mediaservice.shorturl.api.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import java.time.Instant;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ShortUrlResponse {
  private String code;
  private String shortUrl;
  private String mediaId;
  private String assetId;
  private boolean isPublic;
  private Instant createdAt;
  private String createdBy;
  private Instant expiresAt;
  private Instant revokedAt;
  private String label;
}
