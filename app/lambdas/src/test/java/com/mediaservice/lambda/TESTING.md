# Lambda Testing Guide

## Structure

```
src/test/java/com/mediaservice/lambda/
├── handler/         # Handler unit tests using stubs
├── service/         # Service unit tests
└── integration/     # LocalStack integration tests
```

## Key Patterns

### Stub Pattern (preferred over Mockito)
Lambda handlers test using lightweight stubs instead of Mockito:

```java
class StubDynamoDbService extends DynamoDbService {
    private Media storedMedia;

    @Override
    public Optional<Media> getMedia(String mediaId) {
        return Optional.ofNullable(storedMedia);
    }
}
```

Benefits:
- Works reliably with Java 21+ (no byte-buddy issues)
- Clearer test code - behavior is explicit
- Easier to debug

### Integration Tests
Uses `@Testcontainers` with `@Container` annotation for automatic lifecycle:

```java
@Container
static LocalStackContainer localStack = new LocalStackContainer(...)
    .withServices(S3, DYNAMODB);
```

### Resource Cleanup
- `@BeforeEach` cleans DynamoDB and S3 between tests
- `@AfterAll` closes static AWS clients

## Running Tests

```bash
mvn test
```

## Event Simulation

SQS events are simulated using `SQSEvent` objects:

```java
var event = new SQSEvent();
var message = new SQSEvent.SQSMessage();
message.setBody(jsonPayload);
event.setRecords(List.of(message));
handler.handleRequest(event, context);
```
