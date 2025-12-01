package com.mediaservice.shared.cache;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;

import static org.mockito.Mockito.*;

@ExtendWith(MockitoExtension.class)
@DisplayName("CacheInvalidationSubscriber")
class CacheInvalidationSubscriberTest {

  @Mock
  private L1CacheManager l1CacheManager;

  private CacheInvalidationSubscriber subscriber;

  @BeforeEach
  void setUp() {
    subscriber = new CacheInvalidationSubscriber(l1CacheManager);
  }

  @Nested
  @DisplayName("onMessage")
  class OnMessage {

    @Test
    @DisplayName("should invalidate all L1 caches for given mediaId")
    void shouldInvalidateAllCaches() {
      subscriber.onMessage("media-123", "cache:invalidate");

      verify(l1CacheManager).invalidateAll("media-123");
    }

    @Test
    @DisplayName("should trim whitespace from mediaId")
    void shouldTrimWhitespace() {
      subscriber.onMessage("  media-123  ", "cache:invalidate");

      verify(l1CacheManager).invalidateAll("media-123");
    }

    @Test
    @DisplayName("should ignore null message")
    void shouldIgnoreNullMessage() {
      subscriber.onMessage(null, "cache:invalidate");

      verifyNoInteractions(l1CacheManager);
    }

    @Test
    @DisplayName("should ignore empty message")
    void shouldIgnoreEmptyMessage() {
      subscriber.onMessage("", "cache:invalidate");

      verifyNoInteractions(l1CacheManager);
    }

    @Test
    @DisplayName("should ignore blank message")
    void shouldIgnoreBlankMessage() {
      subscriber.onMessage("   ", "cache:invalidate");

      verifyNoInteractions(l1CacheManager);
    }

    @Test
    @DisplayName("should handle cache invalidation exception gracefully")
    void shouldHandleExceptionGracefully() {
      doThrow(new RuntimeException("Cache error"))
          .when(l1CacheManager).invalidateAll("media-123");

      // Should not throw
      subscriber.onMessage("media-123", "cache:invalidate");

      verify(l1CacheManager).invalidateAll("media-123");
    }
  }
}
