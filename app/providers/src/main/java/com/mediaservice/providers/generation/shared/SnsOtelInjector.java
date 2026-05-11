package com.mediaservice.providers.generation.shared;

import io.opentelemetry.api.OpenTelemetry;
import io.opentelemetry.context.Context;
import io.opentelemetry.context.propagation.TextMapSetter;
import java.util.HashMap;
import java.util.Map;
import software.amazon.awssdk.services.sns.model.MessageAttributeValue;

/**
 * Shared OpenTelemetry trace-context injector for SNS message attributes. Lets API and Lambda
 * publishers carry the same trace context across the SNS→SQS→worker hop without each call site
 * re-implementing the {@link TextMapSetter}.
 */
public final class SnsOtelInjector {
  private static final TextMapSetter<Map<String, MessageAttributeValue>> SETTER =
      (carrier, key, value) -> carrier.put(key, MessageAttributeValue.builder()
          .dataType("String")
          .stringValue(value)
          .build());

  private SnsOtelInjector() {
  }

  /** Inject trace context into a fresh attribute map; never returns null. */
  public static Map<String, MessageAttributeValue> injectContext(OpenTelemetry openTelemetry) {
    Map<String, MessageAttributeValue> attributes = new HashMap<>();
    if (openTelemetry != null) {
      openTelemetry.getPropagators().getTextMapPropagator()
          .inject(Context.current(), attributes, SETTER);
    }
    return attributes;
  }
}
