package com.mediaservice.shared.cache;

import com.mediaservice.shared.config.properties.MultiLevelCacheProperties;
import com.mediaservice.shared.http.error.CacheLoadTimeoutException;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.ValueOperations;

import java.time.Duration;
import java.util.Optional;
import java.util.concurrent.atomic.AtomicInteger;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;
import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.anyString;
import static org.mockito.ArgumentMatchers.eq;
import static org.mockito.Mockito.never;
import static org.mockito.Mockito.verify;
import static org.mockito.Mockito.when;

@ExtendWith(MockitoExtension.class)
class SingleFlightServiceTest {

  @Mock
  private StringRedisTemplate stringRedisTemplate;

  @Mock
  private ValueOperations<String, String> valueOperations;

  private MultiLevelCacheProperties cacheProperties;
  private SingleFlightService singleFlightService;

  @BeforeEach
  void setUp() {
    cacheProperties = new MultiLevelCacheProperties();
    cacheProperties.setEnabled(true);

    var singleFlightConfig = new MultiLevelCacheProperties.SingleFlightConfig();
    singleFlightConfig.setEnabled(true);
    singleFlightConfig.setLockTtlMs(5000);
    singleFlightConfig.setMaxRetries(3);
    singleFlightConfig.setRetryIntervalMs(10); // Short for tests
    singleFlightConfig.setLockPrefix("singleflight:");
    cacheProperties.setSingleFlight(singleFlightConfig);

    singleFlightService = new SingleFlightService(stringRedisTemplate, cacheProperties);
  }

  @Nested
  @DisplayName("execute")
  class Execute {

    @Test
    @DisplayName("should load from source when lock acquired")
    void shouldLoadFromSourceWhenLockAcquired() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.setIfAbsent(eq("singleflight:testKey"), eq("1"), any(Duration.class)))
          .thenReturn(true);

      AtomicInteger loadCount = new AtomicInteger(0);
      AtomicInteger cacheWriteCount = new AtomicInteger(0);

      Optional<String> result = singleFlightService.execute(
          "testKey",
          () -> {
            loadCount.incrementAndGet();
            return Optional.of("loaded-value");
          },
          key -> Optional.empty(),
          (key, value) -> cacheWriteCount.incrementAndGet());

      assertThat(result).isPresent().contains("loaded-value");
      assertThat(loadCount.get()).isEqualTo(1);
      assertThat(cacheWriteCount.get()).isEqualTo(1);
      verify(stringRedisTemplate).delete("singleflight:testKey");
    }

    @Test
    @DisplayName("should return empty when data does not exist")
    void shouldReturnEmptyWhenDataNotExist() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.setIfAbsent(anyString(), anyString(), any(Duration.class)))
          .thenReturn(true);

      Optional<String> result = singleFlightService.execute(
          "testKey",
          Optional::empty,
          key -> Optional.empty(),
          (key, value) -> {
          });

      assertThat(result).isEmpty();
    }

    @Test
    @DisplayName("should wait for cache when lock not acquired")
    void shouldWaitForCacheWhenLockNotAcquired() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.setIfAbsent(anyString(), anyString(), any(Duration.class)))
          .thenReturn(false);

      // Simulate cache being populated after some retries
      AtomicInteger readAttempts = new AtomicInteger(0);

      Optional<String> result = singleFlightService.execute(
          "testKey",
          () -> Optional.of("should-not-be-called"),
          key -> {
            if (readAttempts.incrementAndGet() >= 2) {
              return Optional.of("cached-value");
            }
            return Optional.empty();
          },
          (key, value) -> {
          });

      assertThat(result).isPresent().contains("cached-value");
      assertThat(readAttempts.get()).isGreaterThanOrEqualTo(2);
    }

    @Test
    @DisplayName("should return empty when lock released but cache empty")
    void shouldReturnEmptyWhenLockReleasedButCacheEmpty() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.setIfAbsent(anyString(), anyString(), any(Duration.class)))
          .thenReturn(false);
      when(stringRedisTemplate.hasKey("singleflight:testKey"))
          .thenReturn(true)
          .thenReturn(false); // Lock released

      Optional<String> result = singleFlightService.execute(
          "testKey",
          () -> Optional.of("should-not-be-called"),
          key -> Optional.empty(),
          (key, value) -> {
          });

      assertThat(result).isEmpty();
    }

    @Test
    @DisplayName("should throw timeout exception when max retries exceeded")
    void shouldThrowTimeoutWhenMaxRetriesExceeded() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.setIfAbsent(anyString(), anyString(), any(Duration.class)))
          .thenReturn(false);
      when(stringRedisTemplate.hasKey("singleflight:testKey"))
          .thenReturn(true); // Lock always held

      assertThatThrownBy(() -> singleFlightService.execute(
          "testKey",
          () -> Optional.of("should-not-be-called"),
          key -> Optional.empty(),
          (key, value) -> {
          }))
          .isInstanceOf(CacheLoadTimeoutException.class)
          .hasMessageContaining("testKey");
    }

    @Test
    @DisplayName("should load directly when single-flight disabled")
    void shouldLoadDirectlyWhenDisabled() {
      cacheProperties.setEnabled(false);

      AtomicInteger loadCount = new AtomicInteger(0);

      Optional<String> result = singleFlightService.execute(
          "testKey",
          () -> {
            loadCount.incrementAndGet();
            return Optional.of("direct-value");
          },
          key -> Optional.empty(),
          (key, value) -> {
          });

      assertThat(result).isPresent().contains("direct-value");
      assertThat(loadCount.get()).isEqualTo(1);
      verify(stringRedisTemplate, never()).opsForValue();
    }

    @Test
    @DisplayName("should release lock even when loader throws exception")
    void shouldReleaseLockOnException() {
      when(stringRedisTemplate.opsForValue()).thenReturn(valueOperations);
      when(valueOperations.setIfAbsent(anyString(), anyString(), any(Duration.class)))
          .thenReturn(true);

      assertThatThrownBy(() -> singleFlightService.execute(
          "testKey",
          () -> {
            throw new RuntimeException("Loader error");
          },
          key -> Optional.empty(),
          (key, value) -> {
          }))
          .isInstanceOf(RuntimeException.class)
          .hasMessageContaining("Loader error");

      verify(stringRedisTemplate).delete("singleflight:testKey");
    }
  }

  @Nested
  @DisplayName("isLoadInProgress")
  class IsLoadInProgress {

    @Test
    @DisplayName("should return true when lock exists")
    void shouldReturnTrueWhenLockExists() {
      when(stringRedisTemplate.hasKey("singleflight:testKey")).thenReturn(true);

      assertThat(singleFlightService.isLoadInProgress("testKey")).isTrue();
    }

    @Test
    @DisplayName("should return false when lock does not exist")
    void shouldReturnFalseWhenLockNotExists() {
      when(stringRedisTemplate.hasKey("singleflight:testKey")).thenReturn(false);

      assertThat(singleFlightService.isLoadInProgress("testKey")).isFalse();
    }

    @Test
    @DisplayName("should return false when disabled")
    void shouldReturnFalseWhenDisabled() {
      cacheProperties.setEnabled(false);

      assertThat(singleFlightService.isLoadInProgress("testKey")).isFalse();
      verify(stringRedisTemplate, never()).hasKey(anyString());
    }
  }
}
