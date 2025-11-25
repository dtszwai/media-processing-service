package com.mediaservice.config;

import io.opentelemetry.api.OpenTelemetry;
import io.opentelemetry.api.common.Attributes;
import io.opentelemetry.api.metrics.Meter;
import io.opentelemetry.api.trace.Tracer;
import io.opentelemetry.sdk.OpenTelemetrySdk;
import io.opentelemetry.sdk.metrics.SdkMeterProvider;
import io.opentelemetry.sdk.resources.Resource;
import io.opentelemetry.sdk.trace.SdkTracerProvider;
import io.opentelemetry.semconv.ResourceAttributes;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

@Configuration
public class OpenTelemetryConfig {

  @Value("${otel.service.name:media-service}")
  private String serviceName;

  @Bean
  public OpenTelemetry openTelemetry() {
    Resource resource = Resource.getDefault()
        .merge(Resource.create(Attributes.of(
            ResourceAttributes.SERVICE_NAME, serviceName)));

    SdkTracerProvider tracerProvider = SdkTracerProvider.builder()
        .setResource(resource)
        .build();

    SdkMeterProvider meterProvider = SdkMeterProvider.builder()
        .setResource(resource)
        .build();

    return OpenTelemetrySdk.builder()
        .setTracerProvider(tracerProvider)
        .setMeterProvider(meterProvider)
        .buildAndRegisterGlobal();
  }

  @Bean
  public Tracer tracer(OpenTelemetry openTelemetry) {
    return openTelemetry.getTracer("media-service-media-upload");
  }

  @Bean
  public Meter meter(OpenTelemetry openTelemetry) {
    return openTelemetry.getMeter("media-service-media-upload");
  }
}
