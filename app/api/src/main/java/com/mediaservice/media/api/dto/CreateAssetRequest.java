package com.mediaservice.media.api.dto;

import jakarta.validation.constraints.NotEmpty;
import java.util.List;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class CreateAssetRequest {
  private String sourceAssetId;

  @NotEmpty(message = "outputs must not be empty")
  private List<CreateAssetOutput> outputs;
}
