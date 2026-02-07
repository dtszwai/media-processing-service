package com.mediaservice.auth.infrastructure.persistence;

import com.mediaservice.auth.domain.model.ApiKey;
import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.shared.persistence.AbstractDynamoDbRepository;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Repository;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.QueryRequest;

import java.time.Instant;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Optional;

@Slf4j
@Repository
public class ApiKeyDynamoDbRepository extends AbstractDynamoDbRepository<ApiKey> {
  private static final String PK_PREFIX = "APIKEY#";
  private static final String TENANT_INDEX = "tenantId-index";

  public ApiKeyDynamoDbRepository(
      DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    super(dynamoDbClient, tableName);
  }

  @Override
  public ApiKey mapFromItem(Map<String, AttributeValue> item) {
    var pk = getString(item, "PK");
    var keyId = pk.replace(PK_PREFIX, "");
    var scopesAttr = item.get("scopes");
    List<String> scopes = scopesAttr != null && scopesAttr.hasSs() ? scopesAttr.ss() : List.of("USER");

    return ApiKey.builder()
        .keyId(keyId)
        .tenantId(getString(item, "tenantId"))
        .hashedKey(getString(item, "hashedKey"))
        .name(getString(item, "name"))
        .scopes(scopes)
        .createdAt(getInstant(item, "createdAt"))
        .build();
  }

  @Override
  public Map<String, AttributeValue> mapToItem(ApiKey apiKey) {
    var now = Instant.now();
    var item = new HashMap<String, AttributeValue>();
    item.put("PK", s(PK_PREFIX + apiKey.getKeyId()));
    item.put("SK", s(StorageConstants.DYNAMO_SK_METADATA));
    item.put("tenantId", s(apiKey.getTenantId()));
    item.put("hashedKey", s(apiKey.getHashedKey()));
    if (apiKey.getName() != null) {
      item.put("name", s(apiKey.getName()));
    }
    item.put("scopes", AttributeValue.builder().ss(apiKey.getScopes()).build());
    item.put("createdAt", s(apiKey.getCreatedAt() != null ? apiKey.getCreatedAt().toString() : now.toString()));
    return item;
  }

  public void createApiKey(ApiKey apiKey) {
    save(apiKey);
    log.info("Created API key: {} for tenant: {}", apiKey.getKeyId(), apiKey.getTenantId());
  }

  public Optional<ApiKey> getApiKey(String keyPrefix) {
    return findByKey(PK_PREFIX + keyPrefix, StorageConstants.DYNAMO_SK_METADATA);
  }

  public List<ApiKey> getApiKeysByTenant(String tenantId) {
    var request = QueryRequest.builder()
        .tableName(tableName)
        .indexName(TENANT_INDEX)
        .keyConditionExpression("tenantId = :tenantId")
        .filterExpression("begins_with(PK, :pkPrefix)")
        .expressionAttributeValues(Map.of(
            ":tenantId", s(tenantId),
            ":pkPrefix", s(PK_PREFIX)))
        .build();
    var response = dynamoDbClient.query(request);
    return response.items().stream()
        .map(this::mapFromItem)
        .toList();
  }

  public void deleteApiKey(String keyPrefix) {
    delete(PK_PREFIX + keyPrefix, StorageConstants.DYNAMO_SK_METADATA);
    log.info("Deleted API key: {}", keyPrefix);
  }
}
