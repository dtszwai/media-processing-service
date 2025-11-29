# Testing Guide

## Philosophy

- **Unit tests** mock dependencies, run fast, test business logic in isolation
- **Integration tests** use LocalStack (real AWS services), test actual interactions
- **End-to-end tests** verify complete workflows from API to storage

## Test Structure

```
src/test/java/com/mediaservice/
├── controller/          # @WebMvcTest - isolated controller tests
├── service/             # Unit tests with Mockito mocks
├── integration/         # LocalStack-based integration tests
└── config/              # Test configuration
```

## Key Patterns

### Shared LocalStack Container
`LocalStackTestConfig` uses a singleton pattern - one container shared across all integration tests for faster execution:

```java
static {
    localStack = new LocalStackContainer(...)
    localStack.start();
    initializeResources();  // Creates table, bucket, topic once
}
```

### Dynamic Property Injection
Use `@DynamicPropertySource` to inject LocalStack endpoints into Spring context:

```java
@DynamicPropertySource
static void configureProperties(DynamicPropertyRegistry registry) {
    registry.add("aws.dynamodb.endpoint", LocalStackTestConfig::getEndpoint);
}
```

### Test Isolation
`BaseIntegrationTest.cleanUp()` runs `@BeforeEach` to clear DynamoDB and S3, ensuring tests don't affect each other.

### Resource Management
- AWS clients in static blocks use try-with-resources
- Static clients in test classes need `@AfterAll` cleanup
- Always close `ResponseInputStream` from S3 getObject

## Running Tests

```bash
# All tests
mvn test

# Unit tests only
mvn test -Dgroups=\!integration

# Integration tests only
mvn test -Dgroups=integration
```

## Java 21+ Compatibility

Mockito requires these JVM args (configured in pom.xml):
```xml
<argLine>
    --add-opens java.base/java.lang=ALL-UNNAMED
    --add-opens java.base/java.lang.reflect=ALL-UNNAMED
</argLine>
```

## Lambda Tests

Lambda module uses stub implementations instead of Mockito mocks for simpler testing:

```java
class StubDynamoDbService extends DynamoDbService {
    // Override methods with test behavior
}
```

This avoids Mockito complexity with final classes and provides clearer test code.
