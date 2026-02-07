package com.mediaservice.auth.api.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.time.Instant;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
@JsonInclude(JsonInclude.Include.NON_NULL)
public class ApiKeyResponse {
  private String keyId;
  /** Raw key value — only returned on creation, never stored. */
  private String rawKey;
  private String name;
  private Instant createdAt;
}
