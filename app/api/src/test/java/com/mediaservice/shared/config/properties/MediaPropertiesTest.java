package com.mediaservice.shared.config.properties;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import static org.assertj.core.api.Assertions.assertThat;

class MediaPropertiesTest {

  private MediaProperties properties;

  @BeforeEach
  void setUp() {
    properties = new MediaProperties();
    var width = new MediaProperties.Width();
    width.setDefault(500);
    width.setMin(100);
    width.setMax(1024);
    properties.setWidth(width);
    properties.setMaxFileSize(100 * 1024 * 1024);
  }

  @Nested
  @DisplayName("isWidthValid")
  class IsWidthValid {

    @ParameterizedTest
    @ValueSource(ints = { 100, 500, 1024 })
    @DisplayName("should accept valid widths within range")
    void shouldAcceptValidWidths(int width) {
      assertThat(properties.isWidthValid(width)).isTrue();
    }

    @ParameterizedTest
    @ValueSource(ints = { 99, 1025, 0, -1 })
    @DisplayName("should reject widths outside range")
    void shouldRejectInvalidWidths(int width) {
      assertThat(properties.isWidthValid(width)).isFalse();
    }

    @Test
    @DisplayName("should reject null width")
    void shouldRejectNullWidth() {
      assertThat(properties.isWidthValid(null)).isFalse();
    }
  }

  @Nested
  @DisplayName("resolveWidth")
  class ResolveWidth {

    @Test
    @DisplayName("should return provided width when valid")
    void shouldReturnValidWidth() {
      assertThat(properties.resolveWidth(800)).isEqualTo(800);
    }

    @Test
    @DisplayName("should return default width when null")
    void shouldReturnDefaultForNull() {
      assertThat(properties.resolveWidth(null)).isEqualTo(500);
    }

    @Test
    @DisplayName("should return default width when below minimum")
    void shouldReturnDefaultForBelowMin() {
      assertThat(properties.resolveWidth(50)).isEqualTo(500);
    }

    @Test
    @DisplayName("should return default width when above maximum")
    void shouldReturnDefaultForAboveMax() {
      assertThat(properties.resolveWidth(2000)).isEqualTo(500);
    }
  }
}
