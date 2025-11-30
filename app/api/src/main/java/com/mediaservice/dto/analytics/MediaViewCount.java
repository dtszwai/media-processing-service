package com.mediaservice.dto.analytics;

import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

/**
 * View count information for a media item.
 */
@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class MediaViewCount {
  private String mediaId;
  private String name;
  private long viewCount;
  private int rank;
}
