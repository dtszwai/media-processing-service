package com.mediaservice.media.application.mapper;

import com.mediaservice.media.api.dto.MediaAssetResponse;
import com.mediaservice.media.api.dto.MediaResponse;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaAsset;
import org.springframework.stereotype.Component;

@Component
public class MediaMapper {
  public MediaResponse toResponse(Media media) {
    return MediaResponse.builder()
        .mediaId(media.getMediaId())
        .size(media.getSize())
        .name(media.getName())
        .mimetype(media.getMimetype())
        .mediaType(media.getMediaType())
        .source(media.getSource())
        .status(media.getStatus())
        .originalAssetId(media.getOriginalAssetId())
        .createdAt(media.getCreatedAt())
        .updatedAt(media.getUpdatedAt())
        .deletedAt(media.getDeletedAt())
        .documentPageCount(media.getDocumentPageCount())
        .documentTitle(media.getDocumentTitle())
        .documentAuthor(media.getDocumentAuthor())
        .documentSubject(media.getDocumentSubject())
        .documentCreator(media.getDocumentCreator())
        .documentProducer(media.getDocumentProducer())
        .documentCreationDate(media.getDocumentCreationDate())
        .documentModifiedDate(media.getDocumentModifiedDate())
        .documentTextLength(media.getDocumentTextLength())
        .documentTextTruncated(media.getDocumentTextTruncated())
        .build();
  }

  public MediaResponse toResponse(Media media, String thumbnailUrl) {
    MediaResponse response = toResponse(media);
    response.setThumbnailUrl(thumbnailUrl);
    return response;
  }

  public MediaResponse toIdResponse(Media media) {
    return MediaResponse.builder().mediaId(media.getMediaId()).build();
  }

  public MediaResponse toMessageResponse(String message) {
    return MediaResponse.builder().message(message).build();
  }

  public MediaAssetResponse toAssetResponse(MediaAsset asset) {
    return MediaAssetResponse.builder()
        .assetId(asset.getAssetId())
        .mediaId(asset.getMediaId())
        .sourceAssetId(asset.getSourceAssetId())
        .type(asset.getType())
        .tags(asset.getTags())
        .status(asset.getStatus())
        .outputFormat(asset.getOutputFormat())
        .mimetype(asset.getMimetype())
        .size(asset.getSize())
        .width(asset.getWidth())
        .height(asset.getHeight())
        .downloadName(asset.getDownloadName())
        .operation(asset.getOperation())
        .createdAt(asset.getCreatedAt())
        .updatedAt(asset.getUpdatedAt())
        .errorMessage(asset.getErrorMessage())
        .build();
  }
}
