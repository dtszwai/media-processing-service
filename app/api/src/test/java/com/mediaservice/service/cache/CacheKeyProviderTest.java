package com.mediaservice.service.cache;

import com.mediaservice.common.model.OutputFormat;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import static org.assertj.core.api.Assertions.assertThat;

@DisplayName("CacheKeyProvider")
class CacheKeyProviderTest {

  @Test
  @DisplayName("should return all output formats as uppercase strings")
  void getAllFormats_returnsAllFormats() {
    String[] formats = CacheKeyProvider.getAllFormats();

    assertThat(formats).containsExactlyInAnyOrder("JPEG", "PNG", "WEBP");
  }

  @Test
  @DisplayName("should generate media key")
  void mediaKey_generatesCorrectKey() {
    String key = CacheKeyProvider.mediaKey("test-media-id");

    assertThat(key).isEqualTo("test-media-id");
  }

  @Test
  @DisplayName("should generate presigned URL key with string format")
  void presignedUrlKey_withStringFormat_generatesCorrectKey() {
    String key = CacheKeyProvider.presignedUrlKey("test-media-id", "jpeg");

    assertThat(key).isEqualTo("test-media-id:JPEG");
  }

  @Test
  @DisplayName("should generate presigned URL key with OutputFormat enum")
  void presignedUrlKey_withOutputFormat_generatesCorrectKey() {
    String key = CacheKeyProvider.presignedUrlKey("test-media-id", OutputFormat.PNG);

    assertThat(key).isEqualTo("test-media-id:PNG");
  }

  @Test
  @DisplayName("should generate hotkey counter key")
  void hotkeyCounterKey_generatesCorrectKey() {
    String key = CacheKeyProvider.hotkeyCounterKey("test-media-id");

    assertThat(key).isEqualTo("hotkey:counter:test-media-id");
  }

  @Test
  @DisplayName("should generate single-flight lock key")
  void singleFlightLockKey_generatesCorrectKey() {
    String key = CacheKeyProvider.singleFlightLockKey("test-media-id");

    assertThat(key).isEqualTo("singleflight:test-media-id");
  }

  @Test
  @DisplayName("should generate all presigned URL keys for a media item")
  void allPresignedUrlKeys_generatesAllKeys() {
    String[] keys = CacheKeyProvider.allPresignedUrlKeys("test-media-id");

    assertThat(keys).containsExactlyInAnyOrder(
        "test-media-id:JPEG",
        "test-media-id:PNG",
        "test-media-id:WEBP");
  }

  @Test
  @DisplayName("should have correct cache name constants")
  void cacheNameConstants_areCorrect() {
    assertThat(CacheKeyProvider.CACHE_MEDIA).isEqualTo("media");
    assertThat(CacheKeyProvider.CACHE_MEDIA_STATUS).isEqualTo("mediaStatus");
    assertThat(CacheKeyProvider.CACHE_PRESIGNED_URL).isEqualTo("presignedUrl");
    assertThat(CacheKeyProvider.CACHE_PAGINATION).isEqualTo("pagination");
  }

  @Test
  @DisplayName("should have correct Redis key prefix constants")
  void redisKeyPrefixConstants_areCorrect() {
    assertThat(CacheKeyProvider.HOTKEY_COUNTER_PREFIX).isEqualTo("hotkey:counter:");
    assertThat(CacheKeyProvider.HOTKEY_KNOWN_SET).isEqualTo("hotkey:known");
    assertThat(CacheKeyProvider.SINGLE_FLIGHT_PREFIX).isEqualTo("singleflight:");
  }

  @Test
  @DisplayName("should have correct Pub/Sub channel constant")
  void pubSubChannelConstant_isCorrect() {
    assertThat(CacheKeyProvider.CACHE_INVALIDATION_CHANNEL).isEqualTo("cache:invalidate");
  }
}
