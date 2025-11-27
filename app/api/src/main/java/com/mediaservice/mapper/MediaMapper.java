package com.mediaservice.mapper;

import com.mediaservice.dto.MediaResponse;
import com.mediaservice.dto.StatusResponse;
import com.mediaservice.model.Media;
import com.mediaservice.model.MediaStatus;
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
        .build();
  }

  public MediaResponse toIdResponse(Media media) {
    return MediaResponse.builder()
        .mediaId(media.getMediaId())
        .build();
  }

  public MediaResponse toIdResponse(String mediaId) {
    return MediaResponse.builder()
        .mediaId(mediaId)
        .build();
  }

  public MediaResponse toMessageResponse(String message) {
    return MediaResponse.builder()
        .message(message)
        .build();
  }

  public StatusResponse toStatusResponse(MediaStatus status) {
    return StatusResponse.builder()
        .status(status)
        .build();
  }
}
