package com.mediaservice.auth.domain.model;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

import java.time.Instant;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class Tenant {
  private String tenantId;
  private String name;
  private String plan;
  private Instant createdAt;
}
