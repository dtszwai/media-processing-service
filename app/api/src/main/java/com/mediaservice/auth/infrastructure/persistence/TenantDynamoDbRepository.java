package com.mediaservice.auth.infrastructure.persistence;

import com.mediaservice.auth.domain.model.Tenant;
import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.shared.persistence.AbstractDynamoDbRepository;
import lombok.extern.slf4j.Slf4j;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.stereotype.Repository;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;

import java.time.Instant;
import java.util.HashMap;
import java.util.Map;
import java.util.Optional;

@Slf4j
@Repository
public class TenantDynamoDbRepository extends AbstractDynamoDbRepository<Tenant> {
  private static final String PK_PREFIX = "TENANT#";

  public TenantDynamoDbRepository(
      DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    super(dynamoDbClient, tableName);
  }

  @Override
  public Tenant mapFromItem(Map<String, AttributeValue> item) {
    var pk = getString(item, "PK");
    var tenantId = pk.replace(PK_PREFIX, "");
    return Tenant.builder()
        .tenantId(tenantId)
        .name(getString(item, "name"))
        .plan(getString(item, "plan"))
        .createdAt(getInstant(item, "createdAt"))
        .build();
  }

  @Override
  public Map<String, AttributeValue> mapToItem(Tenant tenant) {
    var now = Instant.now();
    var item = new HashMap<String, AttributeValue>();
    item.put("PK", s(PK_PREFIX + tenant.getTenantId()));
    item.put("SK", s(StorageConstants.DYNAMO_SK_METADATA));
    item.put("name", s(tenant.getName()));
    if (tenant.getPlan() != null) {
      item.put("plan", s(tenant.getPlan()));
    }
    item.put("createdAt", s(tenant.getCreatedAt() != null ? tenant.getCreatedAt().toString() : now.toString()));
    return item;
  }

  public void createTenant(Tenant tenant) {
    save(tenant);
    log.info("Created tenant: {}", tenant.getTenantId());
  }

  public Optional<Tenant> getTenant(String tenantId) {
    return findByKey(PK_PREFIX + tenantId, StorageConstants.DYNAMO_SK_METADATA);
  }
}
