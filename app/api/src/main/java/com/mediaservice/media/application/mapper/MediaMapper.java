package com.mediaservice.media.application.mapper;

import com.mediaservice.media.api.dto.MediaResponse;
import com.mediaservice.media.api.dto.StatusResponse;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import org.springframework.stereotype.Component;

@Component
public class MediaMapper {
  public MediaResponse toResponse(Media media) {
    return MediaResponse.builder()
        .mediaId(media.getMediaId())
        .size(media.getSize())
        .name(media.getName())
        .mimetype(media.getMimetype())
        .status(media.getStatus())
        .width(media.getWidth())
        .outputFormat(media.getOutputFormat())
        .createdAt(media.getCreatedAt())
        .updatedAt(media.getUpdatedAt())
        .build();
  }

  public MediaResponse toIdResponse(Media media) {
    return MediaResponse.builder().mediaId(media.getMediaId()).build();
  }

  public MediaResponse toMessageResponse(String message) {
    return MediaResponse.builder().message(message).build();
  }

  public StatusResponse toStatusResponse(MediaStatus status) {
    return StatusResponse.builder().status(status).build();
  }
}
