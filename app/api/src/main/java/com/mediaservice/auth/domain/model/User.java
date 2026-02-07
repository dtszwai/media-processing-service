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
public class User {
  private String userId;
  private String tenantId;
  private String email;
  private String passwordHash;
  private List<String> roles;
  private Instant createdAt;
}
