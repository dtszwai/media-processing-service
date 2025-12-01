package com.mediaservice.integration;

import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.media.infrastructure.persistence.MediaDynamoDbRepository;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Tag;
import org.springframework.beans.factory.annotation.Autowired;

import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.atomic.AtomicInteger;

import static org.assertj.core.api.Assertions.assertThat;

@Tag("integration")
class MediaDynamoDbRepositoryIntegrationTest extends BaseIntegrationTest {

  @Autowired
  private MediaDynamoDbRepository dynamoDbService;

  @Nested
  @DisplayName("CRUD Operations")
  class CrudOperations {

    @Test
    @DisplayName("should create and retrieve media")
    void shouldCreateAndRetrieveMedia() {
      var media = createTestMedia("media-123");

      dynamoDbService.createMedia(media);
      var retrieved = dynamoDbService.getMedia("media-123");

      assertThat(retrieved).isPresent();
      assertThat(retrieved.get().getMediaId()).isEqualTo("media-123");
      assertThat(retrieved.get().getName()).isEqualTo("test.jpg");
      assertThat(retrieved.get().getStatus()).isEqualTo(MediaStatus.PENDING);
    }

    @Test
    @DisplayName("should return empty for non-existent media")
    void shouldReturnEmptyForNonExistent() {
      var result = dynamoDbService.getMedia("nonexistent");

      assertThat(result).isEmpty();
    }

    @Test
    @DisplayName("should retrieve all media sorted by creation time")
    void shouldRetrieveAllMedia() {
      dynamoDbService.createMedia(createTestMedia("media-1"));
      dynamoDbService.createMedia(createTestMedia("media-2"));
      dynamoDbService.createMedia(createTestMedia("media-3"));

      var result = dynamoDbService.getMediaPaginated(null, null);

      assertThat(result.items()).hasSize(3);
    }

    @Test
    @DisplayName("should paginate results with cursor")
    void shouldPaginateWithCursor() {
      // Create 5 media items
      for (int i = 1; i <= 5; i++) {
        dynamoDbService.createMedia(createTestMedia("media-" + i));
      }

      // Get first page with limit 2
      var page1 = dynamoDbService.getMediaPaginated(null, 2);
      assertThat(page1.items()).hasSize(2);
      assertThat(page1.hasMore()).isTrue();
      assertThat(page1.nextCursor()).isNotNull();

      // Get second page using cursor
      var page2 = dynamoDbService.getMediaPaginated(page1.nextCursor(), 2);
      assertThat(page2.items()).hasSize(2);
      assertThat(page2.hasMore()).isTrue();

      // Get third page
      var page3 = dynamoDbService.getMediaPaginated(page2.nextCursor(), 2);
      assertThat(page3.items()).hasSize(1);
      assertThat(page3.hasMore()).isFalse();
      assertThat(page3.nextCursor()).isNull();
    }
  }

  @Nested
  @DisplayName("Status Updates")
  class StatusUpdates {

    @Test
    @DisplayName("should update status")
    void shouldUpdateStatus() {
      var media = createTestMedia("media-123");
      dynamoDbService.createMedia(media);

      dynamoDbService.updateStatus("media-123", MediaStatus.PROCESSING);

      var retrieved = dynamoDbService.getMedia("media-123");
      assertThat(retrieved).isPresent();
      assertThat(retrieved.get().getStatus()).isEqualTo(MediaStatus.PROCESSING);
    }

    @Test
    @DisplayName("should update status conditionally when condition matches")
    void shouldUpdateConditionallyWhenMatches() {
      var media = createTestMedia("media-123");
      dynamoDbService.createMedia(media);

      boolean updated = dynamoDbService.updateStatusConditionally(
          "media-123", MediaStatus.PROCESSING, MediaStatus.PENDING);

      assertThat(updated).isTrue();
      var retrieved = dynamoDbService.getMedia("media-123");
      assertThat(retrieved.get().getStatus()).isEqualTo(MediaStatus.PROCESSING);
    }

    @Test
    @DisplayName("should not update status conditionally when condition fails")
    void shouldNotUpdateConditionallyWhenFails() {
      var media = createTestMedia("media-123");
      dynamoDbService.createMedia(media);
      dynamoDbService.updateStatus("media-123", MediaStatus.PROCESSING);

      boolean updated = dynamoDbService.updateStatusConditionally(
          "media-123", MediaStatus.COMPLETE, MediaStatus.PENDING);

      assertThat(updated).isFalse();
      var retrieved = dynamoDbService.getMedia("media-123");
      assertThat(retrieved.get().getStatus()).isEqualTo(MediaStatus.PROCESSING);
    }
  }

  @Nested
  @DisplayName("Concurrent Operations")
  class ConcurrentOperations {

    @Test
    @DisplayName("should handle concurrent conditional updates correctly")
    void shouldHandleConcurrentUpdates() throws InterruptedException {
      var media = createTestMedia("media-123");
      dynamoDbService.createMedia(media);

      int threadCount = 10;
      var successCount = new AtomicInteger(0);
      var latch = new CountDownLatch(threadCount);

      try (ExecutorService executor = Executors.newFixedThreadPool(threadCount)) {
        for (int i = 0; i < threadCount; i++) {
          executor.submit(() -> {
            try {
              if (dynamoDbService.updateStatusConditionally("media-123", MediaStatus.PROCESSING, MediaStatus.PENDING)) {
                successCount.incrementAndGet();
              }
            } finally {
              latch.countDown();
            }
          });
        }
        latch.await();
      }

      // Only one thread should succeed in the conditional update
      assertThat(successCount.get()).isEqualTo(1);
    }
  }

  private Media createTestMedia(String mediaId) {
    return Media.builder()
        .mediaId(mediaId)
        .name("test.jpg")
        .size(1024L)
        .mimetype("image/jpeg")
        .status(MediaStatus.PENDING)
        .width(500)
        .build();
  }
}
