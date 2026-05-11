package com.mediaservice.providers.generation.core;

import java.time.Instant;
import java.util.HashMap;
import java.util.Map;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;

/**
 * AttributeValue builders + extractors shared by multi-entity DynamoDB repositories in this
 * module. {@code AbstractDynamoDbRepository} in {@code app/api} provides the same helpers for
 * single-entity, generic-typed repos; generation persistence touches multiple entity shapes per
 * row (job / safety / audit / artifact / budget / simulator state) so it can't sit under that
 * generic. Static-import this class to keep the call sites terse.
 */
public final class DynamoDbAttrs {

  private DynamoDbAttrs() {
  }

  public static AttributeValue s(String value) {
    return AttributeValue.builder().s(value != null ? value : "").build();
  }

  public static AttributeValue n(long value) {
    return AttributeValue.builder().n(String.valueOf(value)).build();
  }

  public static AttributeValue n(int value) {
    return AttributeValue.builder().n(String.valueOf(value)).build();
  }

  public static AttributeValue n(String value) {
    return AttributeValue.builder().n(value).build();
  }

  public static AttributeValue bool(boolean value) {
    return AttributeValue.builder().bool(value).build();
  }

  public static AttributeValue stringMap(Map<String, String> value) {
    Map<String, AttributeValue> mapped = new HashMap<>();
    value.forEach((k, v) -> mapped.put(k, s(v)));
    return AttributeValue.builder().m(mapped).build();
  }

  public static String str(Map<String, AttributeValue> item, String key) {
    return item.containsKey(key) && item.get(key).s() != null && !item.get(key).s().isEmpty()
        ? item.get(key).s() : null;
  }

  public static Integer intOrNull(Map<String, AttributeValue> item, String key) {
    return item.containsKey(key) && item.get(key).n() != null ? Integer.parseInt(item.get(key).n()) : null;
  }

  public static Long longOrNull(Map<String, AttributeValue> item, String key) {
    return item.containsKey(key) && item.get(key).n() != null ? Long.parseLong(item.get(key).n()) : null;
  }

  public static Boolean boolOrNull(Map<String, AttributeValue> item, String key) {
    return item.containsKey(key) ? item.get(key).bool() : null;
  }

  public static Instant instantOrNull(Map<String, AttributeValue> item, String key) {
    String value = str(item, key);
    return value != null ? Instant.parse(value) : null;
  }

  public static Instant instantOrNow(Map<String, AttributeValue> item, String key) {
    Instant value = instantOrNull(item, key);
    return value != null ? value : Instant.now();
  }

  public static Map<String, String> stringMapOrEmpty(Map<String, AttributeValue> item, String key) {
    if (!item.containsKey(key) || item.get(key).m() == null || item.get(key).m().isEmpty()) {
      return Map.of();
    }
    Map<String, String> mapped = new HashMap<>();
    item.get(key).m().forEach((k, v) -> {
      if (v.s() != null) {
        mapped.put(k, v.s());
      }
    });
    return mapped;
  }
}
