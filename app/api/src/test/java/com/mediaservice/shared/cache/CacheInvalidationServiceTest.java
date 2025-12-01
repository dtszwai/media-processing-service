package com.mediaservice.shared.cache;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.InOrder;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
@DisplayName("CacheInvalidationService")
class CacheInvalidationServiceTest {

  @Mock
  private L1CacheManager l1CacheManager;

  @Mock
  private L2CacheManager l2CacheManager;

  @Mock
  private CacheInvalidationPublisher invalidationPublisher;

  @Mock
  private HotkeyDetectionService hotkeyDetectionService;

  private CacheInvalidationService service;

  @BeforeEach
  void setUp() {
    service = new CacheInvalidationService(
        l1CacheManager,
        l2CacheManager,
        invalidationPublisher,
        hotkeyDetectionService);
  }

  @Nested
  @DisplayName("invalidateMedia")
  class InvalidateMedia {

    @Test
    @DisplayName("should invalidate L1 cache")
    void shouldInvalidateL1Cache() {
      service.invalidateMedia("media-123");

      verify(l1CacheManager).invalidateAll("media-123");
    }

    @Test
    @DisplayName("should publish invalidation event via Pub/Sub")
    void shouldPublishInvalidationEvent() {
      service.invalidateMedia("media-123");

      verify(invalidationPublisher).publish("media-123");
    }

    @Test
    @DisplayName("should invalidate L2 cache")
    void shouldInvalidateL2Cache() {
      service.invalidateMedia("media-123");

      verify(l2CacheManager).invalidateAll("media-123");
    }

    @Test
    @DisplayName("should clear hotkey counter")
    void shouldClearHotkeyCounter() {
      service.invalidateMedia("media-123");

      verify(hotkeyDetectionService).clearCounter("media-123");
    }

    @Test
    @DisplayName("should invalidate L1 before publishing to Pub/Sub")
    void shouldInvalidateL1BeforePublishing() {
      service.invalidateMedia("media-123");

      InOrder inOrder = inOrder(l1CacheManager, invalidationPublisher);
      inOrder.verify(l1CacheManager).invalidateAll("media-123");
      inOrder.verify(invalidationPublisher).publish("media-123");
    }

    @Test
    @DisplayName("should propagate exception if L1 invalidation fails")
    void shouldPropagateExceptionIfL1Fails() {
      doThrow(new RuntimeException("L1 error"))
          .when(l1CacheManager).invalidateAll("media-123");

      // Exception should propagate
      org.junit.jupiter.api.Assertions.assertThrows(
          RuntimeException.class,
          () -> service.invalidateMedia("media-123"));
    }
  }

  @Nested
  @DisplayName("invalidateMediaStatus")
  class InvalidateMediaStatus {

    @Test
    @DisplayName("should invalidate L2 media status cache")
    void shouldInvalidateL2StatusCache() {
      service.invalidateMediaStatus("media-123");

      verify(l2CacheManager).invalidateMediaStatus("media-123");
    }

    @Test
    @DisplayName("should not invalidate L1 cache")
    void shouldNotInvalidateL1Cache() {
      service.invalidateMediaStatus("media-123");

      verifyNoInteractions(l1CacheManager);
    }
  }

  @Nested
  @DisplayName("invalidatePresignedUrls")
  class InvalidatePresignedUrls {

    @Test
    @DisplayName("should invalidate L1 presigned URLs")
    void shouldInvalidateL1PresignedUrls() {
      service.invalidatePresignedUrls("media-123");

      verify(l1CacheManager).invalidatePresignedUrls("media-123");
    }

    @Test
    @DisplayName("should invalidate L2 presigned URLs")
    void shouldInvalidateL2PresignedUrls() {
      service.invalidatePresignedUrls("media-123");

      verify(l2CacheManager).invalidatePresignedUrls("media-123");
    }
  }

  @Nested
  @DisplayName("invalidatePaginationCache")
  class InvalidatePaginationCache {

    @Test
    @DisplayName("should clear L2 pagination cache")
    void shouldClearL2PaginationCache() {
      service.invalidatePaginationCache();

      verify(l2CacheManager).clearPaginationCache();
    }

    @Test
    @DisplayName("should not interact with L1 cache")
    void shouldNotInteractWithL1Cache() {
      service.invalidatePaginationCache();

      verifyNoInteractions(l1CacheManager);
    }
  }
}
