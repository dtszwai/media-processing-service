package com.mediaservice.shared.cache;

import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.shared.http.error.CacheLoadTimeoutException;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import java.util.Optional;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
@DisplayName("MultiLevelCacheOrchestrator")
class MultiLevelCacheOrchestratorTest {

  @Mock
  private L1CacheManager l1CacheManager;

  @Mock
  private L2CacheManager l2CacheManager;

  @Mock
  private HotkeyDetectionService hotkeyService;

  @Mock
  private SingleFlightService singleFlightService;

  @Mock
  private MediaDynamoDbRepository dynamoDbService;

  private MultiLevelCacheOrchestrator orchestrator;

  private Media testMedia;

  @BeforeEach
  void setUp() {
    orchestrator = new MultiLevelCacheOrchestrator(
        l1CacheManager,
        l2CacheManager,
        hotkeyService,
        singleFlightService,
        dynamoDbService);

    testMedia = Media.builder()
        .mediaId("test-media-id")
        .name("test.jpg")
        .mimetype("image/jpeg")
        .size(1024L)
        .status(MediaStatus.COMPLETE)
        .width(500)
        .outputFormat(OutputFormat.JPEG)
        .build();
  }

  @Nested
  @DisplayName("getMedia")
  class GetMedia {

    @Test
    @DisplayName("should return from L1 cache when present")
    void shouldReturnFromL1CacheWhenPresent() {
      when(l1CacheManager.getMedia("test-media-id")).thenReturn(Optional.of(testMedia));

      Optional<Media> result = orchestrator.getMedia("test-media-id");

      assertThat(result).isPresent().contains(testMedia);
      verify(hotkeyService).recordAccess("test-media-id");
      verifyNoInteractions(l2CacheManager);
    }

    @Test
    @DisplayName("should check L2 cache when L1 miss and populate L1")
    void shouldCheckL2CacheOnL1Miss() {
      when(l1CacheManager.getMedia("test-media-id")).thenReturn(Optional.empty());
      when(l2CacheManager.getMedia("test-media-id")).thenReturn(Optional.of(testMedia));

      Optional<Media> result = orchestrator.getMedia("test-media-id");

      assertThat(result).isPresent().contains(testMedia);
      verify(l1CacheManager).putMedia("test-media-id", testMedia);
      verify(hotkeyService).recordAccess("test-media-id");
    }

    @Test
    @DisplayName("should use single-flight on cache miss")
    void shouldUseSingleFlightOnCacheMiss() {
      when(l1CacheManager.getMedia("test-media-id")).thenReturn(Optional.empty());
      when(l2CacheManager.getMedia("test-media-id")).thenReturn(Optional.empty());
      when(singleFlightService.execute(eq("test-media-id"), any(), any(), any()))
          .thenReturn(Optional.of(testMedia));

      Optional<Media> result = orchestrator.getMedia("test-media-id");

      assertThat(result).isPresent().contains(testMedia);
      verify(singleFlightService).execute(eq("test-media-id"), any(), any(), any());
    }

    @Test
    @DisplayName("should fall back to direct load on single-flight timeout")
    void shouldFallBackOnSingleFlightTimeout() {
      when(l1CacheManager.getMedia("test-media-id")).thenReturn(Optional.empty());
      when(l2CacheManager.getMedia("test-media-id")).thenReturn(Optional.empty());
      when(singleFlightService.execute(eq("test-media-id"), any(), any(), any()))
          .thenThrow(new CacheLoadTimeoutException("Timeout"));
      when(dynamoDbService.getMedia("test-media-id")).thenReturn(Optional.of(testMedia));

      Optional<Media> result = orchestrator.getMedia("test-media-id");

      assertThat(result).isPresent().contains(testMedia);
      verify(dynamoDbService).getMedia("test-media-id");
    }
  }

  @Nested
  @DisplayName("getPresignedUrl")
  class GetPresignedUrl {

    @Test
    @DisplayName("should return from L1 cache when present")
    void shouldReturnFromL1CacheWhenPresent() {
      String cachedUrl = "https://s3.example.com/presigned-url";
      when(l1CacheManager.getPresignedUrl("test-media-id", "JPEG")).thenReturn(Optional.of(cachedUrl));

      Optional<String> result = orchestrator.getPresignedUrl(
          "test-media-id", "JPEG", () -> "new-url");

      assertThat(result).isPresent().contains(cachedUrl);
      verifyNoInteractions(l2CacheManager);
    }

    @Test
    @DisplayName("should check L2 cache and populate L1 on L1 miss")
    void shouldCheckL2AndPopulateL1() {
      String cachedUrl = "https://s3.example.com/cached-url";
      when(l1CacheManager.getPresignedUrl("test-media-id", "JPEG")).thenReturn(Optional.empty());
      when(l2CacheManager.getPresignedUrl("test-media-id", "JPEG")).thenReturn(Optional.of(cachedUrl));

      Optional<String> result = orchestrator.getPresignedUrl(
          "test-media-id", "JPEG", () -> "should-not-be-called");

      assertThat(result).isPresent().contains(cachedUrl);
      verify(l1CacheManager).putPresignedUrl("test-media-id", "JPEG", cachedUrl);
    }

    @Test
    @DisplayName("should generate and cache URL on miss")
    void shouldGenerateAndCacheUrlOnMiss() {
      when(l1CacheManager.getPresignedUrl("test-media-id", "JPEG")).thenReturn(Optional.empty());
      when(l2CacheManager.getPresignedUrl("test-media-id", "JPEG")).thenReturn(Optional.empty());

      String newUrl = "https://s3.example.com/new-presigned-url";
      Optional<String> result = orchestrator.getPresignedUrl(
          "test-media-id", "JPEG", () -> newUrl);

      assertThat(result).isPresent().contains(newUrl);
      verify(l2CacheManager).putPresignedUrl("test-media-id", "JPEG", newUrl);
      verify(l1CacheManager).putPresignedUrl("test-media-id", "JPEG", newUrl);
    }

    @Test
    @DisplayName("should return empty when generator returns null")
    void shouldReturnEmptyWhenGeneratorReturnsNull() {
      when(l1CacheManager.getPresignedUrl("test-media-id", "JPEG")).thenReturn(Optional.empty());
      when(l2CacheManager.getPresignedUrl("test-media-id", "JPEG")).thenReturn(Optional.empty());

      Optional<String> result = orchestrator.getPresignedUrl(
          "test-media-id", "JPEG", () -> null);

      assertThat(result).isEmpty();
      verify(l2CacheManager, never()).putPresignedUrl(any(), any(), any());
    }
  }

  @Nested
  @DisplayName("invalidateLocal")
  class InvalidateLocal {

    @Test
    @DisplayName("should invalidate L1 cache")
    void shouldInvalidateL1Cache() {
      orchestrator.invalidateLocal("test-media-id");

      verify(l1CacheManager).invalidateAll("test-media-id");
    }

    @Test
    @DisplayName("should clear hotkey counter")
    void shouldClearHotkeyCounter() {
      orchestrator.invalidateLocal("test-media-id");

      verify(hotkeyService).clearCounter("test-media-id");
    }
  }

  @Nested
  @DisplayName("getL1Stats")
  class GetL1Stats {

    @Test
    @DisplayName("should delegate to L1CacheManager")
    void shouldDelegateToL1CacheManager() {
      when(l1CacheManager.getStats()).thenReturn("L1 Stats");

      String stats = orchestrator.getL1Stats();

      assertThat(stats).isEqualTo("L1 Stats");
    }
  }
}
