package com.mediaservice.media.application;

import com.mediaservice.analytics.application.AnalyticsService;
import com.mediaservice.common.model.AssetOperation;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.AssetType;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.MediaType;
import com.mediaservice.media.api.dto.CreateAssetOutput;
import com.mediaservice.media.api.dto.CreateAssetRequest;
import com.mediaservice.media.domain.service.DocumentValidationService;
import com.mediaservice.media.domain.service.ImageValidationService;
import com.mediaservice.media.domain.service.MediaTypeResolver;
import com.mediaservice.media.infrastructure.messaging.MediaEventPublisher;
import com.mediaservice.media.infrastructure.persistence.MediaAssetDynamoDbRepository;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import com.mediaservice.media.infrastructure.persistence.ProcessingJobDynamoDbRepository;
import com.mediaservice.media.infrastructure.storage.S3StorageService;
import com.mediaservice.shared.auth.AuthorizationService;
import com.mediaservice.shared.cache.CacheInvalidationService;
import com.mediaservice.shared.cache.MultiLevelCacheOrchestrator;
import com.mediaservice.shared.config.properties.MediaProperties;
import io.opentelemetry.api.metrics.LongCounter;
import io.opentelemetry.api.metrics.LongCounterBuilder;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Tracer;
import java.util.List;
import java.util.Optional;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
class MediaApplicationServiceTest {

  @Mock private MediaDynamoDbRepository mediaRepository;
  @Mock private MediaAssetDynamoDbRepository assetRepository;
  @Mock private ProcessingJobDynamoDbRepository jobRepository;
  @Mock private S3StorageService s3Service;
  @Mock private MediaEventPublisher eventPublisher;
  @Mock private ImageValidationService imageValidationService;
  @Mock private DocumentValidationService documentValidationService;
  @Mock private CacheInvalidationService cacheInvalidationService;
  @Mock private MultiLevelCacheOrchestrator cacheOrchestrator;
  @Mock private AnalyticsService analyticsService;
  @Mock private AuthorizationService authorizationService;
  @Mock private Tracer tracer;
  @Mock private Meter meter;
  @Mock private LongCounterBuilder counterBuilder;
  @Mock private LongCounter counter;

  private MediaApplicationService mediaService;

  @BeforeEach
  void setUp() {
    MediaProperties mediaProperties = new MediaProperties();
    MediaProperties.Width width = new MediaProperties.Width();
    width.setDefault(500);
    width.setMin(100);
    width.setMax(1024);
    mediaProperties.setWidth(width);

    when(meter.counterBuilder(anyString())).thenReturn(counterBuilder);
    when(counterBuilder.setDescription(anyString())).thenReturn(counterBuilder);
    when(counterBuilder.build()).thenReturn(counter);

    mediaService = new MediaApplicationService(
        mediaRepository,
        assetRepository,
        jobRepository,
        s3Service,
        eventPublisher,
        mediaProperties,
        imageValidationService,
        documentValidationService,
        new MediaTypeResolver(),
        cacheInvalidationService,
        cacheOrchestrator,
        analyticsService,
        authorizationService,
        tracer,
        meter);
  }

  @Nested
  @DisplayName("createAssets")
  class CreateAssets {

    @Test
    @DisplayName("reuses matching asset instead of creating a duplicate")
    void reusesMatchingAsset() {
      Media media = Media.builder()
          .mediaId("media-1")
          .tenantId("tenant-1")
          .mediaType(MediaType.IMAGE)
          .status(MediaStatus.COMPLETE)
          .name("file.jpg")
          .originalAssetId("orig-1")
          .build();

      MediaAsset sourceAsset = MediaAsset.builder()
          .assetId("orig-1")
          .mediaId("media-1")
          .type(AssetType.ORIGINAL)
          .status(AssetStatus.COMPLETE)
          .tags(List.of("original"))
          .build();

      MediaAsset existingPreview = MediaAsset.builder()
          .assetId("asset-prev")
          .mediaId("media-1")
          .sourceAssetId("orig-1")
          .operation(AssetOperation.IMAGE_PREVIEW)
          .outputFormat("jpeg")
          .width(500)
          .tags(List.of("preview"))
          .status(AssetStatus.COMPLETE)
          .build();

      when(mediaRepository.getMedia("media-1")).thenReturn(Optional.of(media));
      when(assetRepository.getAsset("media-1", "orig-1")).thenReturn(Optional.of(sourceAsset));
      when(assetRepository.listAssets("media-1")).thenReturn(List.of(sourceAsset, existingPreview));

      CreateAssetRequest request = CreateAssetRequest.builder()
          .outputs(List.of(CreateAssetOutput.builder()
              .operation(AssetOperation.IMAGE_PREVIEW)
              .outputFormat("jpeg")
              .width(500)
              .tags(List.of("preview"))
              .build()))
          .build();

      List<MediaAsset> result = mediaService.createAssets("media-1", request);

      assertThat(result).hasSize(1);
      assertThat(result.get(0)).isEqualTo(existingPreview);

      verify(assetRepository, never()).createAsset(any());
      verify(jobRepository, never()).createJob(any());
      verify(eventPublisher, never()).publishProcessingJob(any(), anyString());
      verify(mediaRepository, never()).updateStatus(anyString(), any());
      verify(cacheInvalidationService, never()).invalidateMedia(anyString());
    }

    @Test
    @DisplayName("creates new asset when no match exists")
    void createsNewAssetWhenNoMatch() {
      Media media = Media.builder()
          .mediaId("media-1")
          .tenantId("tenant-1")
          .mediaType(MediaType.IMAGE)
          .status(MediaStatus.COMPLETE)
          .name("file.jpg")
          .originalAssetId("orig-1")
          .build();

      MediaAsset sourceAsset = MediaAsset.builder()
          .assetId("orig-1")
          .mediaId("media-1")
          .type(AssetType.ORIGINAL)
          .status(AssetStatus.COMPLETE)
          .tags(List.of("original"))
          .build();

      MediaAsset existingPreview = MediaAsset.builder()
          .assetId("asset-prev")
          .mediaId("media-1")
          .sourceAssetId("orig-1")
          .operation(AssetOperation.IMAGE_PREVIEW)
          .outputFormat("jpeg")
          .width(800)
          .tags(List.of("preview"))
          .status(AssetStatus.COMPLETE)
          .build();

      when(mediaRepository.getMedia("media-1")).thenReturn(Optional.of(media));
      when(assetRepository.getAsset("media-1", "orig-1")).thenReturn(Optional.of(sourceAsset));
      when(assetRepository.listAssets("media-1")).thenReturn(List.of(sourceAsset, existingPreview));

      CreateAssetRequest request = CreateAssetRequest.builder()
          .outputs(List.of(CreateAssetOutput.builder()
              .operation(AssetOperation.IMAGE_PREVIEW)
              .outputFormat("jpeg")
              .width(500)
              .tags(List.of("preview"))
              .build()))
          .build();

      List<MediaAsset> result = mediaService.createAssets("media-1", request);

      assertThat(result).hasSize(1);
      assertThat(result.get(0).getOperation()).isEqualTo(AssetOperation.IMAGE_PREVIEW);
      assertThat(result.get(0).getWidth()).isEqualTo(500);

      verify(assetRepository).createAsset(any());
      verify(jobRepository).createJob(any());
      verify(eventPublisher).publishProcessingJob(any(), eq("image"));
      verify(mediaRepository).updateStatus("media-1", MediaStatus.PROCESSING);
      verify(cacheInvalidationService).invalidateMedia("media-1");
    }
  }
}
