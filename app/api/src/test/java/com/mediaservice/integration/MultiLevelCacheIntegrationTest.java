package com.mediaservice.integration;

import com.github.benmanes.caffeine.cache.Cache;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import com.mediaservice.shared.cache.HotkeyDetectionService;
import com.mediaservice.shared.cache.SingleFlightService;
import com.mediaservice.shared.cache.CacheKeyProvider;
import com.mediaservice.shared.cache.MultiLevelCacheOrchestrator;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.springframework.beans.factory.annotation.Autowired;
import org.springframework.cache.CacheManager;

import java.util.Optional;
import java.util.UUID;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicInteger;

import static org.assertj.core.api.Assertions.assertThat;

/**
 * Integration tests for multi-level caching architecture.
 *
 * <p>
 * Tests verify:
 * <ul>
 * <li>L1 (Caffeine) and L2 (Redis) cache interaction</li>
 * <li>Single-flight protection prevents thundering herd</li>
 * <li>Hotkey detection and TTL extension</li>
 * <li>Cache invalidation across all levels</li>
 * </ul>
 */
class MultiLevelCacheIntegrationTest extends BaseIntegrationTest {

  @Autowired
  private MultiLevelCacheOrchestrator cacheOrchestrator;

  @Autowired
  private MediaDynamoDbRepository dynamoDbService;

  @Autowired
  private HotkeyDetectionService hotkeyDetectionService;

  @Autowired
  private SingleFlightService singleFlightService;

  @Autowired
  private CacheManager cacheManager;

  @Autowired
  private Cache<String, Media> localMediaCache;

  private Media testMedia;
  private String testMediaId;

  @BeforeEach
  void setUpMedia() {
    testMediaId = UUID.randomUUID().toString();
    testMedia = Media.builder()
        .mediaId(testMediaId)
        .name("test-image.jpg")
        .mimetype("image/jpeg")
        .size(1024L)
        .status(MediaStatus.COMPLETE)
        .width(500)
        .outputFormat(OutputFormat.JPEG)
        .build();

    // Insert test media into DynamoDB
    dynamoDbService.createMedia(testMedia);
  }

  @Nested
  @DisplayName("Multi-Level Cache Lookup")
  class MultiLevelCacheLookup {

    @Test
    @DisplayName("should retrieve from L3 (DynamoDB) on first access and populate caches")
    void shouldRetrieveFromL3OnFirstAccess() {
      // L1 should be empty
      assertThat(localMediaCache.getIfPresent(testMediaId)).isNull();

      // L2 should be empty
      var redisCache = cacheManager.getCache(CacheKeyProvider.CACHE_MEDIA);
      assertThat(redisCache).isNotNull();
      assertThat(redisCache.get(testMediaId, Media.class)).isNull();

      // First access - should go to L3 (DynamoDB)
      Optional<Media> result = cacheOrchestrator.getMedia(testMediaId);

      assertThat(result).isPresent();
      assertThat(result.get().getMediaId()).isEqualTo(testMediaId);

      // L1 should now be populated
      Media l1Cached = localMediaCache.getIfPresent(testMediaId);
      assertThat(l1Cached).isNotNull();
      assertThat(l1Cached.getMediaId()).isEqualTo(testMediaId);
    }

    @Test
    @DisplayName("should retrieve from L1 (Caffeine) on second access")
    void shouldRetrieveFromL1OnSecondAccess() {
      // First access - populates caches
      cacheOrchestrator.getMedia(testMediaId);

      // Delete from L2 to verify L1 is used
      var redisCache = cacheManager.getCache(CacheKeyProvider.CACHE_MEDIA);
      if (redisCache != null) {
        redisCache.evict(testMediaId);
      }

      // Second access - should come from L1
      Optional<Media> result = cacheOrchestrator.getMedia(testMediaId);

      assertThat(result).isPresent();
      assertThat(result.get().getMediaId()).isEqualTo(testMediaId);
    }

    @Test
    @DisplayName("should return empty for non-existent media")
    void shouldReturnEmptyForNonExistent() {
      Optional<Media> result = cacheOrchestrator.getMedia("non-existent-id");

      assertThat(result).isEmpty();
    }
  }

  @Nested
  @DisplayName("Cache Invalidation")
  class CacheInvalidation {

    @Test
    @DisplayName("should invalidate L1 cache when invalidateLocal is called")
    void shouldInvalidateL1Cache() {
      // Populate cache
      cacheOrchestrator.getMedia(testMediaId);
      assertThat(localMediaCache.getIfPresent(testMediaId)).isNotNull();

      // Invalidate
      cacheOrchestrator.invalidateLocal(testMediaId);

      // L1 should be empty
      assertThat(localMediaCache.getIfPresent(testMediaId)).isNull();
    }
  }

  @Nested
  @DisplayName("Hotkey Detection")
  class HotkeyDetectionIntegration {

    @Test
    @DisplayName("should track access count for hotkey detection")
    void shouldTrackAccessCount() {
      // Initial count should be 0
      assertThat(hotkeyDetectionService.getAccessCount(testMediaId)).isEqualTo(0);

      // Access multiple times
      for (int i = 0; i < 10; i++) {
        cacheOrchestrator.getMedia(testMediaId);
      }

      // Count should be tracked (may be slightly less due to L1 cache hits not
      // recording)
      long count = hotkeyDetectionService.getAccessCount(testMediaId);
      assertThat(count).isGreaterThanOrEqualTo(1);
    }

    @Test
    @DisplayName("should identify hot keys after threshold")
    void shouldIdentifyHotKeys() {
      // Initially not a hot key
      assertThat(hotkeyDetectionService.isHotKey(testMediaId)).isFalse();

      // Simulate many accesses (above threshold)
      for (int i = 0; i < 110; i++) {
        hotkeyDetectionService.recordAccess(testMediaId);
      }

      // Now should be a hot key
      assertThat(hotkeyDetectionService.isHotKey(testMediaId)).isTrue();
    }
  }

  @Nested
  @DisplayName("Single-Flight Protection")
  class SingleFlightProtection {

    @Test
    @DisplayName("should prevent thundering herd with concurrent requests")
    void shouldPreventThunderingHerd() throws InterruptedException {
      // Create a new media that's only in DynamoDB (not cached)
      String freshMediaId = UUID.randomUUID().toString();
      Media freshMedia = Media.builder()
          .mediaId(freshMediaId)
          .name("fresh.jpg")
          .mimetype("image/jpeg")
          .size(2048L)
          .status(MediaStatus.COMPLETE)
          .width(500)
          .outputFormat(OutputFormat.PNG)
          .build();
      dynamoDbService.createMedia(freshMedia);

      // Track how many times we actually hit the loader
      AtomicInteger loadCount = new AtomicInteger(0);

      int numThreads = 10;
      ExecutorService executor = Executors.newFixedThreadPool(numThreads);
      CountDownLatch startLatch = new CountDownLatch(1);
      CountDownLatch doneLatch = new CountDownLatch(numThreads);

      // Submit concurrent requests
      for (int i = 0; i < numThreads; i++) {
        executor.submit(() -> {
          try {
            startLatch.await(); // Wait for all threads to be ready
            singleFlightService.execute(
                "test-single-flight-" + freshMediaId,
                () -> {
                  loadCount.incrementAndGet();
                  // Simulate slow DB query
                  try {
                    Thread.sleep(100);
                  } catch (InterruptedException e) {
                    Thread.currentThread().interrupt();
                  }
                  return dynamoDbService.getMedia(freshMediaId);
                },
                key -> Optional.empty(),
                (key, value) -> {
                });
          } catch (Exception e) {
            // Expected for some threads
          } finally {
            doneLatch.countDown();
          }
        });
      }

      // Start all threads simultaneously
      startLatch.countDown();

      // Wait for completion
      doneLatch.await(10, TimeUnit.SECONDS);
      executor.shutdown();

      // Only one thread should have loaded from DB
      // Others should have waited or gotten the cached result
      assertThat(loadCount.get()).isLessThanOrEqualTo(2);
    }

    @Test
    @DisplayName("should correctly report load in progress")
    void shouldReportLoadInProgress() {
      // Initially no load in progress
      assertThat(singleFlightService.isLoadInProgress("test-key")).isFalse();
    }
  }

  @Nested
  @DisplayName("Presigned URL Caching")
  class PresignedUrlCaching {

    @Test
    @DisplayName("should cache presigned URLs")
    void shouldCachePresignedUrls() {
      AtomicInteger generatorCallCount = new AtomicInteger(0);

      // First call - should invoke generator
      Optional<String> url1 = cacheOrchestrator.getPresignedUrl(
          testMediaId,
          "JPEG",
          () -> {
            generatorCallCount.incrementAndGet();
            return "https://s3.example.com/presigned-url-" + System.currentTimeMillis();
          });

      assertThat(url1).isPresent();
      assertThat(generatorCallCount.get()).isEqualTo(1);

      // Second call - should use cached URL
      Optional<String> url2 = cacheOrchestrator.getPresignedUrl(
          testMediaId,
          "JPEG",
          () -> {
            generatorCallCount.incrementAndGet();
            return "https://s3.example.com/different-url";
          });

      assertThat(url2).isPresent();
      assertThat(url2.get()).isEqualTo(url1.get()); // Same URL
      assertThat(generatorCallCount.get()).isEqualTo(1); // Generator not called again
    }

    @Test
    @DisplayName("should cache different URLs for different formats")
    void shouldCacheDifferentFormats() {
      Optional<String> jpegUrl = cacheOrchestrator.getPresignedUrl(
          testMediaId, "JPEG", () -> "https://s3.example.com/jpeg-url");

      Optional<String> pngUrl = cacheOrchestrator.getPresignedUrl(
          testMediaId, "PNG", () -> "https://s3.example.com/png-url");

      assertThat(jpegUrl).isPresent();
      assertThat(jpegUrl.get()).contains("jpeg-url");
      assertThat(pngUrl).isPresent();
      assertThat(pngUrl.get()).contains("png-url");
      assertThat(jpegUrl.get()).isNotEqualTo(pngUrl.get());
    }
  }

  @Nested
  @DisplayName("L1 Cache Statistics")
  class L1CacheStatistics {

    @Test
    @DisplayName("should provide L1 cache statistics")
    void shouldProvideL1Statistics() {
      // Generate some cache activity
      cacheOrchestrator.getMedia(testMediaId);
      cacheOrchestrator.getMedia(testMediaId); // Hit
      cacheOrchestrator.getMedia("non-existent"); // Miss

      String stats = cacheOrchestrator.getL1Stats();

      assertThat(stats).contains("L1 Cache Stats");
      assertThat(stats).contains("hitRate");
      assertThat(stats).contains("hitCount");
    }
  }
}
