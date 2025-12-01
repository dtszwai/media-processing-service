package com.mediaservice.media.api.dto;

import com.mediaservice.common.model.MediaStatus;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class StatusResponse {
  private MediaStatus status;
}
