package com.mediaservice.shared.auth;

import com.mediaservice.common.constants.StorageConstants;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.GetItemRequest;

import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.HexFormat;
import java.util.List;
import java.util.Map;
import java.util.Optional;

@Slf4j
@Service
public class ApiKeyService {
  private static final String PK_PREFIX = "APIKEY#";
  private static final int KEY_PREFIX_LENGTH = 8;

  private final DynamoDbClient dynamoDbClient;
  private final String tableName;
  private final AuthProperties authProperties;

  public ApiKeyService(
      DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName,
      AuthProperties authProperties) {
    this.dynamoDbClient = dynamoDbClient;
    this.tableName = tableName;
    this.authProperties = authProperties;
  }

  public Optional<AuthPrincipal> validateApiKey(String rawKey) {
    if (!authProperties.getApiKey().isEnabled()) {
      return Optional.empty();
    }
    if (rawKey == null || rawKey.length() < KEY_PREFIX_LENGTH) {
      return Optional.empty();
    }

    String keyPrefix = rawKey.substring(0, KEY_PREFIX_LENGTH);
    String hashedKey = hashKey(rawKey);

    try {
      var request = GetItemRequest.builder()
          .tableName(tableName)
          .key(Map.of(
              "PK", AttributeValue.builder().s(PK_PREFIX + keyPrefix).build(),
              "SK", AttributeValue.builder().s(StorageConstants.DYNAMO_SK_METADATA).build()))
          .build();

      var response = dynamoDbClient.getItem(request);
      if (!response.hasItem() || response.item().isEmpty()) {
        log.debug("API key not found for prefix: {}", keyPrefix);
        return Optional.empty();
      }

      var item = response.item();
      String storedHash = item.get("hashedKey").s();
      if (!storedHash.equals(hashedKey)) {
        log.debug("API key hash mismatch for prefix: {}", keyPrefix);
        return Optional.empty();
      }

      String tenantId = item.get("tenantId").s();
      String name = item.containsKey("name") ? item.get("name").s() : "api-key";
      @SuppressWarnings("unchecked")
      List<String> scopes = item.containsKey("scopes") ? item.get("scopes").ss() : List.of("USER");

      return Optional.of(new AuthPrincipal(
          tenantId,
          "apikey:" + keyPrefix,
          name,
          scopes,
          AuthPrincipal.AuthMethod.API_KEY));
    } catch (Exception e) {
      log.error("Failed to validate API key: {}", e.getMessage());
      return Optional.empty();
    }
  }

  public static String hashKey(String rawKey) {
    try {
      var digest = MessageDigest.getInstance("SHA-256");
      byte[] hash = digest.digest(rawKey.getBytes(StandardCharsets.UTF_8));
      return HexFormat.of().formatHex(hash);
    } catch (NoSuchAlgorithmException e) {
      throw new IllegalStateException("SHA-256 not available", e);
    }
  }

  public static String extractPrefix(String rawKey) {
    if (rawKey == null || rawKey.length() < KEY_PREFIX_LENGTH) {
      throw new IllegalArgumentException("API key too short");
    }
    return rawKey.substring(0, KEY_PREFIX_LENGTH);
  }
}
