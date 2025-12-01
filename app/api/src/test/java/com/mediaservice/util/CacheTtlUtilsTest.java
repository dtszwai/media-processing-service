package com.mediaservice.util;

import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.RepeatedTest;
import org.junit.jupiter.api.Test;

import java.time.Duration;
import java.util.HashSet;
import java.util.Set;

import static org.assertj.core.api.Assertions.assertThat;

class CacheTtlUtilsTest {

  @Nested
  @DisplayName("withJitter")
  class WithJitter {

    @Test
    @DisplayName("should return null for null input")
    void shouldReturnNullForNullInput() {
      assertThat(CacheTtlUtils.withJitter(null, 0.1)).isNull();
    }

    @Test
    @DisplayName("should return same duration for zero TTL")
    void shouldReturnSameForZeroTtl() {
      Duration zero = Duration.ZERO;
      assertThat(CacheTtlUtils.withJitter(zero, 0.1)).isEqualTo(zero);
    }

    @Test
    @DisplayName("should return same duration for negative TTL")
    void shouldReturnSameForNegativeTtl() {
      Duration negative = Duration.ofSeconds(-10);
      assertThat(CacheTtlUtils.withJitter(negative, 0.1)).isEqualTo(negative);
    }

    @Test
    @DisplayName("should return same duration for zero jitter")
    void shouldReturnSameForZeroJitter() {
      Duration base = Duration.ofMinutes(5);
      assertThat(CacheTtlUtils.withJitter(base, 0)).isEqualTo(base);
    }

    @Test
    @DisplayName("should return same duration for negative jitter")
    void shouldReturnSameForNegativeJitter() {
      Duration base = Duration.ofMinutes(5);
      assertThat(CacheTtlUtils.withJitter(base, -0.1)).isEqualTo(base);
    }

    @RepeatedTest(10)
    @DisplayName("should apply jitter within expected range")
    void shouldApplyJitterWithinRange() {
      Duration base = Duration.ofSeconds(300); // 5 minutes
      double jitterPercent = 0.1; // ±10%

      Duration result = CacheTtlUtils.withJitter(base, jitterPercent);

      // Should be within ±10% of base
      long minExpected = (long) (300 * 0.9); // 270
      long maxExpected = (long) (300 * 1.1); // 330

      assertThat(result.getSeconds())
          .isBetween(minExpected, maxExpected);
    }

    @Test
    @DisplayName("should produce varying results (not deterministic)")
    void shouldProduceVaryingResults() {
      Duration base = Duration.ofSeconds(300);
      Set<Long> results = new HashSet<>();

      // Run 100 times and collect unique results
      for (int i = 0; i < 100; i++) {
        results.add(CacheTtlUtils.withJitter(base, 0.1).toMillis());
      }

      // Should have multiple unique values (allowing for some randomness)
      assertThat(results.size()).isGreaterThan(5);
    }

    @Test
    @DisplayName("should clamp jitter to 50% maximum")
    void shouldClampJitterTo50Percent() {
      Duration base = Duration.ofSeconds(100);
      Duration result = CacheTtlUtils.withJitter(base, 0.9); // 90% jitter

      // Should be clamped to ±50%
      assertThat(result.getSeconds())
          .isBetween(50L, 150L);
    }

    @Test
    @DisplayName("should work with default 10% jitter")
    void shouldWorkWithDefaultJitter() {
      Duration base = Duration.ofSeconds(100);
      Duration result = CacheTtlUtils.withJitter(base);

      // Default is 10%
      assertThat(result.getSeconds())
          .isBetween(90L, 110L);
    }
  }

  @Nested
  @DisplayName("withJitterSeconds")
  class WithJitterSeconds {

    @Test
    @DisplayName("should return seconds with jitter applied")
    void shouldReturnSecondsWithJitter() {
      long result = CacheTtlUtils.withJitterSeconds(100, 0.1);

      assertThat(result).isBetween(90L, 110L);
    }
  }

  @Nested
  @DisplayName("extendForHotKey")
  class ExtendForHotKey {

    @Test
    @DisplayName("should return null for null input")
    void shouldReturnNullForNullInput() {
      assertThat(CacheTtlUtils.extendForHotKey(null, 3)).isNull();
    }

    @Test
    @DisplayName("should return same duration for multiplier <= 1")
    void shouldReturnSameForLowMultiplier() {
      Duration base = Duration.ofMinutes(5);

      assertThat(CacheTtlUtils.extendForHotKey(base, 1)).isEqualTo(base);
      assertThat(CacheTtlUtils.extendForHotKey(base, 0)).isEqualTo(base);
      assertThat(CacheTtlUtils.extendForHotKey(base, -1)).isEqualTo(base);
    }

    @Test
    @DisplayName("should multiply TTL by multiplier")
    void shouldMultiplyTtl() {
      Duration base = Duration.ofMinutes(5);
      Duration result = CacheTtlUtils.extendForHotKey(base, 3);

      assertThat(result).isEqualTo(Duration.ofMinutes(15));
    }

    @Test
    @DisplayName("should work with various multipliers")
    void shouldWorkWithVariousMultipliers() {
      Duration base = Duration.ofSeconds(100);

      assertThat(CacheTtlUtils.extendForHotKey(base, 2).getSeconds()).isEqualTo(200);
      assertThat(CacheTtlUtils.extendForHotKey(base, 5).getSeconds()).isEqualTo(500);
      assertThat(CacheTtlUtils.extendForHotKey(base, 10).getSeconds()).isEqualTo(1000);
    }
  }
}
