package com.mediaservice.service.cache;

import com.github.benmanes.caffeine.cache.Cache;
import com.github.benmanes.caffeine.cache.Caffeine;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.config.MultiLevelCacheProperties;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;

import java.util.Optional;

import static org.assertj.core.api.Assertions.assertThat;

@DisplayName("L1CacheManager")
class L1CacheManagerTest {

  private Cache<String, Media> mediaCache;
  private Cache<String, String> presignedUrlCache;
  private MultiLevelCacheProperties cacheProperties;
  private L1CacheManager l1CacheManager;

  @BeforeEach
  void setUp() {
    mediaCache = Caffeine.newBuilder().recordStats().build();
    presignedUrlCache = Caffeine.newBuilder().recordStats().build();
    cacheProperties = new MultiLevelCacheProperties();
    cacheProperties.setEnabled(true);
    cacheProperties.getLocal().setEnabled(true);
    cacheProperties.getLocal().setRecordStats(true);

    l1CacheManager = new L1CacheManager(mediaCache, presignedUrlCache, cacheProperties);
  }

  @Nested
  @DisplayName("when L1 cache is enabled")
  class WhenEnabled {

    @Test
    @DisplayName("should return true for isEnabled")
    void isEnabled_returnsTrue() {
      assertThat(l1CacheManager.isEnabled()).isTrue();
    }

    @Test
    @DisplayName("should cache and retrieve media")
    void putAndGetMedia_cachesCorrectly() {
      Media media = createTestMedia("test-id");

      l1CacheManager.putMedia("test-id", media);
      Optional<Media> result = l1CacheManager.getMedia("test-id");

      assertThat(result).isPresent().contains(media);
    }

    @Test
    @DisplayName("should return empty when media not in cache")
    void getMedia_whenNotInCache_returnsEmpty() {
      Optional<Media> result = l1CacheManager.getMedia("non-existent");

      assertThat(result).isEmpty();
    }

    @Test
    @DisplayName("should invalidate media from cache")
    void invalidateMedia_removesFromCache() {
      Media media = createTestMedia("test-id");
      l1CacheManager.putMedia("test-id", media);

      l1CacheManager.invalidateMedia("test-id");
      Optional<Media> result = l1CacheManager.getMedia("test-id");

      assertThat(result).isEmpty();
    }

    @Test
    @DisplayName("should cache and retrieve presigned URL")
    void putAndGetPresignedUrl_cachesCorrectly() {
      String url = "https://example.com/presigned";

      l1CacheManager.putPresignedUrl("test-id", "JPEG", url);
      Optional<String> result = l1CacheManager.getPresignedUrl("test-id", "JPEG");

      assertThat(result).isPresent().contains(url);
    }

    @Test
    @DisplayName("should invalidate presigned URLs for all formats")
    void invalidatePresignedUrls_removesAllFormats() {
      l1CacheManager.putPresignedUrl("test-id", "JPEG", "url1");
      l1CacheManager.putPresignedUrl("test-id", "PNG", "url2");
      l1CacheManager.putPresignedUrl("test-id", "WEBP", "url3");

      l1CacheManager.invalidatePresignedUrls("test-id");

      assertThat(l1CacheManager.getPresignedUrl("test-id", "JPEG")).isEmpty();
      assertThat(l1CacheManager.getPresignedUrl("test-id", "PNG")).isEmpty();
      assertThat(l1CacheManager.getPresignedUrl("test-id", "WEBP")).isEmpty();
    }

    @Test
    @DisplayName("should invalidate all caches for media item")
    void invalidateAll_removesMediaAndPresignedUrls() {
      Media media = createTestMedia("test-id");
      l1CacheManager.putMedia("test-id", media);
      l1CacheManager.putPresignedUrl("test-id", "JPEG", "url");

      l1CacheManager.invalidateAll("test-id");

      assertThat(l1CacheManager.getMedia("test-id")).isEmpty();
      assertThat(l1CacheManager.getPresignedUrl("test-id", "JPEG")).isEmpty();
    }

    @Test
    @DisplayName("should return cache statistics")
    void getStats_returnsStatistics() {
      l1CacheManager.putMedia("test-id", createTestMedia("test-id"));
      l1CacheManager.getMedia("test-id"); // hit
      l1CacheManager.getMedia("non-existent"); // miss

      String stats = l1CacheManager.getStats();

      assertThat(stats).contains("L1 Cache Stats");
      assertThat(stats).contains("hitRate");
      assertThat(stats).contains("hitCount");
      assertThat(stats).contains("missCount");
    }

    @Test
    @DisplayName("should return cache sizes")
    void getCacheSize_returnsCorrectSizes() {
      l1CacheManager.putMedia("test-1", createTestMedia("test-1"));
      l1CacheManager.putMedia("test-2", createTestMedia("test-2"));
      l1CacheManager.putPresignedUrl("test-1", "JPEG", "url1");

      assertThat(l1CacheManager.getMediaCacheSize()).isEqualTo(2);
      assertThat(l1CacheManager.getPresignedUrlCacheSize()).isEqualTo(1);
    }
  }

  @Nested
  @DisplayName("when L1 cache is disabled")
  class WhenDisabled {

    @BeforeEach
    void setUp() {
      cacheProperties.getLocal().setEnabled(false);
    }

    @Test
    @DisplayName("should return false for isEnabled")
    void isEnabled_returnsFalse() {
      assertThat(l1CacheManager.isEnabled()).isFalse();
    }

    @Test
    @DisplayName("should return empty when getting media")
    void getMedia_returnsEmpty() {
      // Pre-populate cache directly
      mediaCache.put("test-id", createTestMedia("test-id"));

      Optional<Media> result = l1CacheManager.getMedia("test-id");

      assertThat(result).isEmpty();
    }

    @Test
    @DisplayName("should not cache media when putting")
    void putMedia_doesNotCache() {
      l1CacheManager.putMedia("test-id", createTestMedia("test-id"));

      assertThat(mediaCache.getIfPresent("test-id")).isNull();
    }

    @Test
    @DisplayName("should return empty when getting presigned URL")
    void getPresignedUrl_returnsEmpty() {
      presignedUrlCache.put("test-id:JPEG", "url");

      Optional<String> result = l1CacheManager.getPresignedUrl("test-id", "JPEG");

      assertThat(result).isEmpty();
    }
  }

  @Nested
  @DisplayName("when stats recording is disabled")
  class WhenStatsDisabled {

    @BeforeEach
    void setUp() {
      cacheProperties.getLocal().setRecordStats(false);
    }

    @Test
    @DisplayName("should return disabled message for stats")
    void getStats_returnsDisabledMessage() {
      String stats = l1CacheManager.getStats();

      assertThat(stats).isEqualTo("Stats recording disabled");
    }
  }

  private Media createTestMedia(String mediaId) {
    return Media.builder()
        .mediaId(mediaId)
        .name("test.jpg")
        .status(MediaStatus.COMPLETE)
        .build();
  }
}
