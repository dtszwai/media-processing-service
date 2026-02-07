package com.mediaservice.auth.infrastructure.persistence;

import com.mediaservice.auth.domain.model.User;
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
public class UserDynamoDbRepository extends AbstractDynamoDbRepository<User> {
  private static final String PK_PREFIX = "USER#";
  private static final String EMAIL_INDEX = "email-index";

  public UserDynamoDbRepository(
      DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    super(dynamoDbClient, tableName);
  }

  @Override
  public User mapFromItem(Map<String, AttributeValue> item) {
    var pk = getString(item, "PK");
    var userId = pk.replace(PK_PREFIX, "");
    var rolesAttr = item.get("roles");
    List<String> roles = rolesAttr != null && rolesAttr.hasSs() ? rolesAttr.ss() : List.of("USER");

    return User.builder()
        .userId(userId)
        .tenantId(getString(item, "tenantId"))
        .email(getString(item, "email"))
        .passwordHash(getString(item, "passwordHash"))
        .roles(roles)
        .createdAt(getInstant(item, "createdAt"))
        .build();
  }

  @Override
  public Map<String, AttributeValue> mapToItem(User user) {
    var now = Instant.now();
    var item = new HashMap<String, AttributeValue>();
    item.put("PK", s(PK_PREFIX + user.getUserId()));
    item.put("SK", s(StorageConstants.DYNAMO_SK_METADATA));
    item.put("tenantId", s(user.getTenantId()));
    item.put("email", s(user.getEmail()));
    item.put("passwordHash", s(user.getPasswordHash()));
    item.put("roles", AttributeValue.builder().ss(user.getRoles()).build());
    item.put("createdAt", s(user.getCreatedAt() != null ? user.getCreatedAt().toString() : now.toString()));
    return item;
  }

  public void createUser(User user) {
    save(user);
    log.info("Created user: {} for tenant: {}", user.getUserId(), user.getTenantId());
  }

  public Optional<User> getUser(String userId) {
    return findByKey(PK_PREFIX + userId, StorageConstants.DYNAMO_SK_METADATA);
  }

  public Optional<User> findByEmail(String email) {
    var request = QueryRequest.builder()
        .tableName(tableName)
        .indexName(EMAIL_INDEX)
        .keyConditionExpression("email = :email")
        .expressionAttributeValues(Map.of(":email", s(email)))
        .limit(1)
        .build();
    var response = dynamoDbClient.query(request);
    if (response.items().isEmpty()) {
      return Optional.empty();
    }
    return Optional.of(mapFromItem(response.items().getFirst()));
  }
}
