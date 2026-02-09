package com.mediaservice.shorturl.domain.model;

import com.mediaservice.common.model.ShortUrlVariant;
import java.time.Instant;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class ShortUrl {
  private String code;
  private String tenantId;
  private String mediaId;
  private ShortUrlVariant variant;
  private boolean isPublic;
  private Instant createdAt;
  private String createdBy;
  private Instant expiresAt;
  private Instant revokedAt;
  private String label;
}
