package com.mediaservice.shared.persistence;

import software.amazon.awssdk.services.dynamodb.model.AttributeValue;

import java.util.List;
import java.util.Map;
import java.util.Optional;

/**
 * Repository interface for DynamoDB operations.
 *
 * <p>
 * Defines the contract for DynamoDB repositories following the Repository
 * pattern.
 * Implementations can be domain-specific (e.g., MediaRepository,
 * AnalyticsRepository)
 * or use the generic implementation for simple cases.
 *
 * @param <T> The entity type this repository manages
 */
public interface DynamoDbRepository<T> {

  /**
   * Find an entity by its partition key and sort key.
   *
   * @param pk Partition key value
   * @param sk Sort key value
   * @return Optional containing the entity if found
   */
  Optional<T> findByKey(String pk, String sk);

  /**
   * Save an entity to DynamoDB.
   *
   * @param entity The entity to save
   */
  void save(T entity);

  /**
   * Delete an entity by its partition key and sort key.
   *
   * @param pk Partition key value
   * @param sk Sort key value
   */
  void delete(String pk, String sk);

  /**
   * Query items by partition key.
   *
   * @param pk            Partition key value
   * @param keyExpression Key condition expression
   * @param limit         Maximum number of items to return
   * @return List of entities matching the query
   */
  List<T> queryByPartitionKey(String pk, String keyExpression, int limit);

  /**
   * Convert a DynamoDB item to the entity type.
   *
   * @param item DynamoDB item as attribute map
   * @return The mapped entity
   */
  T mapFromItem(Map<String, AttributeValue> item);

  /**
   * Convert an entity to a DynamoDB item.
   *
   * @param entity The entity to convert
   * @return DynamoDB item as attribute map
   */
  Map<String, AttributeValue> mapToItem(T entity);
}
