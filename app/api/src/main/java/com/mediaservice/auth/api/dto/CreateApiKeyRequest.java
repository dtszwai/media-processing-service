package com.mediaservice.auth.api.dto;

import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Size;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class CreateApiKeyRequest {
  @NotBlank(message = "API key name is required")
  @Size(min = 1, max = 100, message = "Name must be between 1 and 100 characters")
  private String name;
}
