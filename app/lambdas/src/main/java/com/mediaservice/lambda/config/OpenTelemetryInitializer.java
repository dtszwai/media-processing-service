package com.mediaservice.lambda.config;

import io.opentelemetry.api.OpenTelemetry;
import io.opentelemetry.instrumentation.log4j.appender.v2_17.OpenTelemetryAppender;
import io.opentelemetry.sdk.OpenTelemetrySdk;
import io.opentelemetry.sdk.autoconfigure.AutoConfiguredOpenTelemetrySdk;

public class OpenTelemetryInitializer {
  private static volatile OpenTelemetrySdk openTelemetrySdk;

  private OpenTelemetryInitializer() {
  }

  public static synchronized OpenTelemetry initialize() {
    if (openTelemetrySdk != null) {
      return openTelemetrySdk;
    }
    openTelemetrySdk = AutoConfiguredOpenTelemetrySdk.initialize().getOpenTelemetrySdk();
    OpenTelemetryAppender.install(openTelemetrySdk);
    return openTelemetrySdk;
  }

  public static void flush() {
    if (openTelemetrySdk != null) {
      openTelemetrySdk.getSdkTracerProvider().forceFlush();
      openTelemetrySdk.getSdkMeterProvider().forceFlush();
      openTelemetrySdk.getSdkLoggerProvider().forceFlush();
    }
  }
}
