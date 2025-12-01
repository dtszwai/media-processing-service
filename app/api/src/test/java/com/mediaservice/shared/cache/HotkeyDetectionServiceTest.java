package com.mediaservice.shared.cache;

import com.mediaservice.shared.config.properties.MultiLevelCacheProperties;
import com.mediaservice.analytics.api.dto.Period;
import com.mediaservice.analytics.api.dto.EntityViewCount;
import com.mediaservice.analytics.application.AnalyticsService;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.beans.factory.ObjectProvider;
import org.springframework.data.redis.core.SetOperations;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.ValueOperations;

import java.time.Duration;
import java.util.List;
import java.util.Set;

import static org.assertj.core.api.Assertions.assertThat;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyInt;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

@ExtendWith(MockitoExtension.class)
class HotkeyDetectionServiceTest {

  @Mock
  private StringRedisTemplate stringRedisTemplate;

  @Mock
  private ValueOperations<String, String> valueOperations;

  @Mock
  private SetOperations<String, String> setOperations;

  @Mock
  private ObjectProvider<AnalyticsService> analyticsServiceProvider;

  @Mock
  private AnalyticsService analyticsService;

  private MultiLevelCacheProperties cacheProperties;
  private HotkeyDetectionService hotkeyService;

  @BeforeEach
  void setUp() {
    cacheProperties = new MultiLevelCacheProperties();
    cacheProperties.setEnabled(true);

    var hotkeyConfig = new MultiLevelCacheProperties.HotkeyConfig();
    hotkeyConfig.setEnabled(true);
    hotkeyConfig.setThreshold(100);
    hotkeyConfig.setWindowSeconds(60);
    hotkeyConfig.setTtlMultiplier(3);
    hotkeyConfig.setKeyPrefix("hotkey:counter:");
    hotkeyConfig.setKnownHotKeysKey("hotkey:known");

    var warmUpConfig = new MultiLevelCacheProperties.WarmUpConfig();
    warmUpConfig.setEnabled(true);
    warmUpConfig.setTopCount(100);
    warmUpConfig.setRefreshIntervalMinutes(5);
    warmUpConfig.setKnownKeysTtlMinutes(30);
    hotkeyConfig.setWarmUp(warmUpConfig);

    cacheProperties.setHotkey(hotkeyConfig);

    hotkeyService = new HotkeyDetectionService(stringRedisTemplate, cacheProperties, analyticsServiceProvider);
  }

  @Nested
  @DisplayName("recordAccess")
  class RecordAccess {

    @Test
    @DisplayName("should increment counter and set expiry on first access")
    void shouldIncrementAndSetExpiry() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.increment("hotkey:counter:media123")).thenReturn(1L);

      hotkeyService.recordAccess("media123");

      verify(valueOperations).increment("hotkey:counter:media123");
      verify(stringRedisTemplate).expire(eq("hotkey:counter:media123"), eq(Duration.ofSeconds(60)));
    }

    @Test
    @DisplayName("should not set expiry on subsequent accesses")
    void shouldNotSetExpiryOnSubsequentAccess() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.increment("hotkey:counter:media123")).thenReturn(50L);

      hotkeyService.recordAccess("media123");

      verify(valueOperations).increment("hotkey:counter:media123");
      verify(stringRedisTemplate, never()).expire(anyString(), any(Duration.class));
    }

    @Test
    @DisplayName("should not record when feature is disabled")
    void shouldNotRecordWhenDisabled() {
      cacheProperties.setEnabled(false);

      hotkeyService.recordAccess("media123");

      verify(stringRedisTemplate, never()).opsForValue();
    }

    @Test
    @DisplayName("should not record when hotkey detection is disabled")
    void shouldNotRecordWhenHotkeyDisabled() {
      cacheProperties.getHotkey().setEnabled(false);

      hotkeyService.recordAccess("media123");

      verify(stringRedisTemplate, never()).opsForValue();
    }

    @Test
    @DisplayName("should handle Redis exceptions gracefully")
    void shouldHandleRedisExceptionsGracefully() {
      when(stringRedisTemplate.opsForValue()).thenThrow(new RuntimeException("Redis error"));

      // Should not throw
      hotkeyService.recordAccess("media123");
    }
  }

  @Nested
  @DisplayName("isHotKey")
  class IsHotKey {

    @Test
    @DisplayName("should return true when count exceeds threshold")
    void shouldReturnTrueWhenCountExceedsThreshold() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn("150");

      assertThat(hotkeyService.isHotKey("media123")).isTrue();
    }

    @Test
    @DisplayName("should return true when count equals threshold")
    void shouldReturnTrueWhenCountEqualsThreshold() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn("100");

      assertThat(hotkeyService.isHotKey("media123")).isTrue();
    }

    @Test
    @DisplayName("should return false when count is below threshold")
    void shouldReturnFalseWhenCountBelowThreshold() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn("50");
      when(stringRedisTemplate.opsForSet()).thenReturn(setOperations);
      when(setOperations.isMember("hotkey:known", "media123")).thenReturn(false);

      assertThat(hotkeyService.isHotKey("media123")).isFalse();
    }

    @Test
    @DisplayName("should return false when key does not exist")
    void shouldReturnFalseWhenKeyNotExists() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn(null);
      when(stringRedisTemplate.opsForSet()).thenReturn(setOperations);
      when(setOperations.isMember("hotkey:known", "media123")).thenReturn(false);

      assertThat(hotkeyService.isHotKey("media123")).isFalse();
    }

    @Test
    @DisplayName("should return true when key is in known hot keys set")
    void shouldReturnTrueWhenKeyInKnownHotKeys() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn("10"); // Below threshold
      when(stringRedisTemplate.opsForSet()).thenReturn(setOperations);
      when(setOperations.isMember("hotkey:known", "media123")).thenReturn(true);

      assertThat(hotkeyService.isHotKey("media123")).isTrue();
    }

    @Test
    @DisplayName("should return false when disabled")
    void shouldReturnFalseWhenDisabled() {
      cacheProperties.setEnabled(false);

      assertThat(hotkeyService.isHotKey("media123")).isFalse();
      verify(stringRedisTemplate, never()).opsForValue();
    }

    @Test
    @DisplayName("should handle Redis exceptions gracefully")
    void shouldHandleRedisExceptionsGracefully() {
      when(stringRedisTemplate.opsForValue()).thenThrow(new RuntimeException("Redis error"));

      assertThat(hotkeyService.isHotKey("media123")).isFalse();
    }
  }

  @Nested
  @DisplayName("getAccessCount")
  class GetAccessCount {

    @Test
    @DisplayName("should return count from Redis")
    void shouldReturnCountFromRedis() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn("42");

      assertThat(hotkeyService.getAccessCount("media123")).isEqualTo(42);
    }

    @Test
    @DisplayName("should return 0 when key does not exist")
    void shouldReturnZeroWhenKeyNotExists() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn(null);

      assertThat(hotkeyService.getAccessCount("media123")).isEqualTo(0);
    }

    @Test
    @DisplayName("should return 0 on error")
    void shouldReturnZeroOnError() {
      when(stringRedisTemplate.opsForValue()).thenThrow(new RuntimeException("Redis error"));

      assertThat(hotkeyService.getAccessCount("media123")).isEqualTo(0);
    }
  }

  @Nested
  @DisplayName("getTtlForKey")
  class GetTtlForKey {

    @Test
    @DisplayName("should extend TTL for hot key")
    void shouldExtendTtlForHotKey() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn("150");

      Duration baseTtl = Duration.ofMinutes(5);
      Duration result = hotkeyService.getTtlForKey("media123", baseTtl);

      // Should be 3x (multiplier) = 15 minutes
      assertThat(result).isEqualTo(Duration.ofMinutes(15));
    }

    @Test
    @DisplayName("should return base TTL for cold key")
    void shouldReturnBaseTtlForColdKey() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn("10");
      when(stringRedisTemplate.opsForSet()).thenReturn(setOperations);
      when(setOperations.isMember("hotkey:known", "media123")).thenReturn(false);

      Duration baseTtl = Duration.ofMinutes(5);
      Duration result = hotkeyService.getTtlForKey("media123", baseTtl);

      assertThat(result).isEqualTo(baseTtl);
    }

    @Test
    @DisplayName("should extend TTL for key in known hot keys set")
    void shouldExtendTtlForKnownHotKey() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.get("hotkey:counter:media123")).thenReturn("10"); // Below threshold
      when(stringRedisTemplate.opsForSet()).thenReturn(setOperations);
      when(setOperations.isMember("hotkey:known", "media123")).thenReturn(true);

      Duration baseTtl = Duration.ofMinutes(5);
      Duration result = hotkeyService.getTtlForKey("media123", baseTtl);

      // Should be 3x (multiplier) = 15 minutes
      assertThat(result).isEqualTo(Duration.ofMinutes(15));
    }
  }

  @Nested
  @DisplayName("clearCounter")
  class ClearCounter {

    @Test
    @DisplayName("should delete counter from Redis")
    void shouldDeleteCounterFromRedis() {
      hotkeyService.clearCounter("media123");

      verify(stringRedisTemplate).delete("hotkey:counter:media123");
    }

    @Test
    @DisplayName("should handle exceptions gracefully")
    void shouldHandleExceptionsGracefully() {
      when(stringRedisTemplate.delete("hotkey:counter:media123"))
          .thenThrow(new RuntimeException("Redis error"));

      // Should not throw
      hotkeyService.clearCounter("media123");
    }
  }

  @Nested
  @DisplayName("Warm-up from Analytics")
  class WarmUpFromAnalytics {

    @Test
    @DisplayName("should load hot keys from analytics on refresh")
    void shouldLoadHotKeysFromAnalyticsOnRefresh() {
      when(analyticsServiceProvider.getIfAvailable()).thenReturn(analyticsService);
      when(analyticsService.getTopMedia(Period.TODAY, 100)).thenReturn(List.of(
          EntityViewCount.forMedia("media1", "Media 1", 1000L, 1),
          EntityViewCount.forMedia("media2", "Media 2", 500L, 2)));
      when(analyticsService.getTopMedia(Period.ALL_TIME, 100)).thenReturn(List.of(
          EntityViewCount.forMedia("media1", "Media 1", 5000L, 1),
          EntityViewCount.forMedia("media3", "Media 3", 3000L, 2)));
      when(stringRedisTemplate.opsForSet()).thenReturn(setOperations);

      hotkeyService.refreshKnownHotKeysFromAnalytics();

      // Should add all hot media IDs to temp set (3 unique IDs: media1, media2,
      // media3)
      verify(setOperations).add(anyString(), any(String[].class));
    }

    @Test
    @DisplayName("should not refresh when analytics service not available")
    void shouldNotRefreshWhenAnalyticsNotAvailable() {
      when(analyticsServiceProvider.getIfAvailable()).thenReturn(null);

      hotkeyService.refreshKnownHotKeysFromAnalytics();

      verify(analyticsService, never()).getTopMedia(any(), anyInt());
    }

    @Test
    @DisplayName("should not refresh when warm-up is disabled")
    void shouldNotRefreshWhenWarmUpDisabled() {
      cacheProperties.getHotkey().getWarmUp().setEnabled(false);

      hotkeyService.warmUp();

      verify(analyticsServiceProvider, never()).getIfAvailable();
    }

    @Test
    @DisplayName("should handle empty analytics results")
    void shouldHandleEmptyAnalyticsResults() {
      when(analyticsServiceProvider.getIfAvailable()).thenReturn(analyticsService);
      when(analyticsService.getTopMedia(Period.TODAY, 100)).thenReturn(List.of());
      when(analyticsService.getTopMedia(Period.ALL_TIME, 100)).thenReturn(List.of());

      hotkeyService.refreshKnownHotKeysFromAnalytics();

      // Should not try to add empty set
      verify(stringRedisTemplate, never()).opsForSet();
    }

    @Test
    @DisplayName("should handle analytics exceptions gracefully")
    void shouldHandleAnalyticsExceptionsGracefully() {
      when(analyticsServiceProvider.getIfAvailable()).thenReturn(analyticsService);
      when(analyticsService.getTopMedia(any(), anyInt())).thenThrow(new RuntimeException("Analytics error"));

      // Should not throw
      hotkeyService.refreshKnownHotKeysFromAnalytics();
    }
  }

  @Nested
  @DisplayName("getKnownHotKeys")
  class GetKnownHotKeys {

    @Test
    @DisplayName("should return known hot keys from Redis")
    void shouldReturnKnownHotKeysFromRedis() {
      when(stringRedisTemplate.opsForSet()).thenReturn(setOperations);
      when(setOperations.members("hotkey:known")).thenReturn(Set.of("media1", "media2", "media3"));

      Set<String> result = hotkeyService.getKnownHotKeys();

      assertThat(result).containsExactlyInAnyOrder("media1", "media2", "media3");
    }

    @Test
    @DisplayName("should return empty set when warm-up is disabled")
    void shouldReturnEmptySetWhenWarmUpDisabled() {
      cacheProperties.getHotkey().getWarmUp().setEnabled(false);

      Set<String> result = hotkeyService.getKnownHotKeys();

      assertThat(result).isEmpty();
      verify(stringRedisTemplate, never()).opsForSet();
    }

    @Test
    @DisplayName("should return empty set on error")
    void shouldReturnEmptySetOnError() {
      when(stringRedisTemplate.opsForSet()).thenThrow(new RuntimeException("Redis error"));

      Set<String> result = hotkeyService.getKnownHotKeys();

      assertThat(result).isEmpty();
    }
  }

  @Nested
  @DisplayName("getStats")
  class GetStats {

    @Test
    @DisplayName("should return stats string")
    void shouldReturnStatsString() {
      when(stringRedisTemplate.opsForSet()).thenReturn(setOperations);
      when(setOperations.members("hotkey:known")).thenReturn(Set.of("media1", "media2"));

      String stats = hotkeyService.getStats();

      assertThat(stats).contains("knownHotKeys=2");
      assertThat(stats).contains("threshold=100");
      assertThat(stats).contains("windowSeconds=60");
      assertThat(stats).contains("ttlMultiplier=3");
    }
  }
}
