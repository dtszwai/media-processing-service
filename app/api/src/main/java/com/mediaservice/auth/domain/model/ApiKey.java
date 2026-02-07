package com.mediaservice.auth.domain.model;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.time.Instant;
import java.util.List;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class ApiKey {
  private String keyId;
  private String tenantId;
  private String hashedKey;
  private String name;
  private List<String> scopes;
  private Instant createdAt;
}
