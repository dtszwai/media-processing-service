package com.mediaservice.shared.config;

import io.opentelemetry.api.OpenTelemetry;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Tracer;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class OpenTelemetryConfig {
  @Bean
  public Tracer tracer(OpenTelemetry openTelemetry) {
    return openTelemetry.getTracer("media-service-api");
  }

  @Bean
  public Meter meter(OpenTelemetry openTelemetry) {
    return openTelemetry.getMeter("media-service-api");
  }
}
