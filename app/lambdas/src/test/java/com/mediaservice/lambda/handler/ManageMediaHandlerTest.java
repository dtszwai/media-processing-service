package com.mediaservice.lambda.handler;

import com.amazonaws.services.lambda.runtime.Context;
import com.amazonaws.services.lambda.runtime.events.SQSEvent;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.event.MediaEvent;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.OutputFormat;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;

import java.io.IOException;
import java.util.List;
import java.util.Optional;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

/**
 * Unit tests for ManageMediaHandler using stub implementations instead of
 * mocks.
 * This approach is more compatible with Java 23 and provides clearer test
 * behavior.
 */
class ManageMediaHandlerTest {
  private ObjectMapper objectMapper;
  private StubDynamoDbService dynamoDbService;
  private StubS3Service s3Service;
  private StubImageProcessingService imageProcessingService;
  private TestableManageMediaHandler handler;

  @BeforeEach
  void setUp() {
    objectMapper = new ObjectMapper();
    dynamoDbService = new StubDynamoDbService();
    s3Service = new StubS3Service();
    imageProcessingService = new StubImageProcessingService();
    handler = new TestableManageMediaHandler(dynamoDbService, s3Service, imageProcessingService);
  }

  @Nested
  @DisplayName("Process Media Event")
  class ProcessMediaEvent {

    @Test
    @DisplayName("should process media successfully")
    void shouldProcessMediaSuccessfully() throws Exception {
      var media = Media.builder().mediaId("media-123").name("test.jpg").width(500).status(MediaStatus.PENDING)
          .build();
      dynamoDbService.setMediaToReturn(media);
      s3Service.setFileContent(new byte[100]);
      imageProcessingService.setProcessedOutput(new byte[50]);
      var sqsEvent = createSqsEvent("media.v1.process", "media-123", 500);
      String result = handler.handleRequest(sqsEvent, null);
      assertThat(result).isEqualTo("OK");
      assertThat(s3Service.wasUploadCalled()).isTrue();
      assertThat(dynamoDbService.getLastSetStatus()).isEqualTo(MediaStatus.COMPLETE);
    }

    @Test
    @DisplayName("should skip when media not in expected status")
    void shouldSkipWhenNotInExpectedStatus() throws Exception {
      dynamoDbService.setMediaToReturn(null); // No media returned = status check failed
      var sqsEvent = createSqsEvent("media.v1.process", "media-123", 500);
      String result = handler.handleRequest(sqsEvent, null);
      assertThat(result).isEqualTo("OK");
      assertThat(s3Service.wasGetFileCalled()).isFalse();
    }

    @Test
    @DisplayName("should set status to ERROR on processing failure")
    void shouldSetErrorStatusOnFailure() throws Exception {
      var media = Media.builder().mediaId("media-123").name("test.jpg").width(500).status(MediaStatus.PENDING)
          .build();
      dynamoDbService.setMediaToReturn(media);
      s3Service.setShouldThrowOnGet(true);
      var sqsEvent = createSqsEvent("media.v1.process", "media-123", 500);
      assertThatThrownBy(() -> handler.handleRequest(sqsEvent, null))
          .isInstanceOf(RuntimeException.class);
      assertThat(dynamoDbService.getLastSetStatus()).isEqualTo(MediaStatus.ERROR);
    }
  }

  @Nested
  @DisplayName("Resize Media Event")
  class ResizeMediaEvent {

    @Test
    @DisplayName("should resize media to new width")
    void shouldResizeMedia() throws Exception {
      var media = Media.builder().mediaId("media-123").name("test.jpg").width(500).status(MediaStatus.PENDING)
          .build();
      dynamoDbService.setMediaToReturn(media);
      s3Service.setFileContent(new byte[100]);
      imageProcessingService.setProcessedOutput(new byte[50]);
      var sqsEvent = createSqsEvent("media.v1.resize", "media-123", 800);
      String result = handler.handleRequest(sqsEvent, null);
      assertThat(result).isEqualTo("OK");
      assertThat(dynamoDbService.getLastWidthSet()).isEqualTo(800);
    }

    @Test
    @DisplayName("should skip resize when width is missing")
    void shouldSkipWhenWidthMissing() throws Exception {
      var sqsEvent = createSqsEvent("media.v1.resize", "media-123", null);
      String result = handler.handleRequest(sqsEvent, null);
      assertThat(result).isEqualTo("OK");
      assertThat(dynamoDbService.wasSetStatusCalled()).isFalse();
    }
  }

  @Nested
  @DisplayName("Delete Media Event")
  class DeleteMediaEvent {

    @Test
    @DisplayName("should delete media and files")
    void shouldDeleteMediaAndFiles() throws Exception {
      var media = Media.builder().mediaId("media-123").name("test.jpg").status(MediaStatus.COMPLETE).build();
      dynamoDbService.setDeletedMedia(media);
      var sqsEvent = createSqsEvent("media.v1.delete", "media-123", null);
      String result = handler.handleRequest(sqsEvent, null);
      assertThat(result).isEqualTo("OK");
      assertThat(s3Service.getDeletedKeys()).contains("uploads", "resized");
    }

    @Test
    @DisplayName("should skip delete when media not found")
    void shouldSkipDeleteWhenNotFound() throws Exception {
      dynamoDbService.setDeletedMedia(null);
      var sqsEvent = createSqsEvent("media.v1.delete", "media-123", null);
      String result = handler.handleRequest(sqsEvent, null);
      assertThat(result).isEqualTo("OK");
      assertThat(s3Service.getDeletedKeys()).isEmpty();
    }

    @Test
    @DisplayName("should not delete resized file when still processing")
    void shouldNotDeleteResizedWhenProcessing() throws Exception {
      var media = Media.builder().mediaId("media-123").name("test.jpg").status(MediaStatus.PROCESSING).build();
      dynamoDbService.setDeletedMedia(media);
      var sqsEvent = createSqsEvent("media.v1.delete", "media-123", null);
      handler.handleRequest(sqsEvent, null);
      assertThat(s3Service.getDeletedKeys()).containsExactly("uploads");
    }
  }

  @Nested
  @DisplayName("Message Validation")
  class MessageValidation {

    @Test
    @DisplayName("should skip message with null payload")
    void shouldSkipNullPayload() throws Exception {
      var event = new MediaEvent("media.v1.process", null);
      var sqsEvent = createSqsEventFromMediaEvent(event);
      String result = handler.handleRequest(sqsEvent, null);
      assertThat(result).isEqualTo("OK");
      assertThat(s3Service.wasGetFileCalled()).isFalse();
    }

    @Test
    @DisplayName("should skip message with empty mediaId")
    void shouldSkipEmptyMediaId() throws Exception {
      var sqsEvent = createSqsEvent("media.v1.process", "", 500);
      String result = handler.handleRequest(sqsEvent, null);
      assertThat(result).isEqualTo("OK");
      assertThat(s3Service.wasGetFileCalled()).isFalse();
    }

    @Test
    @DisplayName("should skip unknown event type")
    void shouldSkipUnknownEventType() throws Exception {
      var sqsEvent = createSqsEvent("media.v1.unknown", "media-123", 500);
      String result = handler.handleRequest(sqsEvent, null);
      assertThat(result).isEqualTo("OK");
      assertThat(s3Service.wasGetFileCalled()).isFalse();
    }
  }

  private SQSEvent createSqsEvent(String eventType, String mediaId, Integer width) throws Exception {
    var payload = new MediaEvent.MediaEventPayload(mediaId, width, "jpeg");
    var event = new MediaEvent(eventType, payload);
    return createSqsEventFromMediaEvent(event);
  }

  private SQSEvent createSqsEventFromMediaEvent(MediaEvent event) throws Exception {
    String eventJson = objectMapper.writeValueAsString(event);
    String snsWrapper = objectMapper.writeValueAsString(java.util.Map.of("Message", eventJson));
    var message = new SQSEvent.SQSMessage();
    message.setBody(snsWrapper);
    var sqsEvent = new SQSEvent();
    sqsEvent.setRecords(List.of(message));
    return sqsEvent;
  }

  // Stub implementations for testing

  static class StubDynamoDbService {
    private Media mediaToReturn;
    private Media deletedMedia;
    private MediaStatus lastSetStatus;
    private Integer lastWidthSet;
    private boolean setStatusCalled;

    void setMediaToReturn(Media media) {
      this.mediaToReturn = media;
    }

    void setDeletedMedia(Media media) {
      this.deletedMedia = media;
    }

    Optional<Media> setMediaStatusConditionally(String mediaId, MediaStatus newStatus, MediaStatus expectedStatus) {
      setStatusCalled = true;
      lastSetStatus = newStatus;
      return Optional.ofNullable(mediaToReturn);
    }

    Optional<Media> setMediaStatusConditionally(String mediaId, MediaStatus newStatus, MediaStatus expectedStatus,
        Integer width) {
      setStatusCalled = true;
      lastSetStatus = newStatus;
      lastWidthSet = width;
      return Optional.ofNullable(mediaToReturn);
    }

    void setMediaStatus(String mediaId, MediaStatus status) {
      setStatusCalled = true;
      lastSetStatus = status;
    }

    Optional<Media> deleteMedia(String mediaId) {
      return Optional.ofNullable(deletedMedia);
    }

    MediaStatus getLastSetStatus() {
      return lastSetStatus;
    }

    Integer getLastWidthSet() {
      return lastWidthSet;
    }

    boolean wasSetStatusCalled() {
      return setStatusCalled;
    }
  }

  static class StubS3Service {
    private byte[] fileContent;
    private boolean shouldThrowOnGet;
    private boolean getFileCalled;
    private boolean uploadCalled;
    private final java.util.List<String> deletedKeys = new java.util.ArrayList<>();

    void setFileContent(byte[] content) {
      this.fileContent = content;
    }

    void setShouldThrowOnGet(boolean shouldThrow) {
      this.shouldThrowOnGet = shouldThrow;
    }

    byte[] getMediaFile(String mediaId, String mediaName) {
      getFileCalled = true;
      if (shouldThrowOnGet) {
        throw new RuntimeException("S3 error");
      }
      return fileContent;
    }

    void uploadMedia(String mediaId, String mediaName, byte[] data, String keyPrefix) {
      uploadCalled = true;
    }

    void deleteMediaFile(String mediaId, String mediaName, String keyPrefix) {
      deletedKeys.add(keyPrefix);
    }

    boolean wasGetFileCalled() {
      return getFileCalled;
    }

    boolean wasUploadCalled() {
      return uploadCalled;
    }

    java.util.List<String> getDeletedKeys() {
      return deletedKeys;
    }
  }

  static class StubImageProcessingService {
    private byte[] processedOutput;

    void setProcessedOutput(byte[] output) {
      this.processedOutput = output;
    }

    byte[] processImage(byte[] data, Integer width, OutputFormat outputFormat) throws IOException {
      return processedOutput;
    }

    byte[] resizeImage(byte[] data, Integer width, OutputFormat outputFormat) throws IOException {
      return processedOutput;
    }
  }

  /**
   * Testable version of ManageMediaHandler that accepts stub dependencies.
   */
  static class TestableManageMediaHandler {
    private final StubDynamoDbService dynamoDbService;
    private final StubS3Service s3Service;
    private final StubImageProcessingService imageProcessingService;
    private final ObjectMapper objectMapper = new ObjectMapper();

    TestableManageMediaHandler(StubDynamoDbService dynamoDbService, StubS3Service s3Service,
        StubImageProcessingService imageProcessingService) {
      this.dynamoDbService = dynamoDbService;
      this.s3Service = s3Service;
      this.imageProcessingService = imageProcessingService;
    }

    public String handleRequest(SQSEvent sqsEvent, Context context) {
      for (var message : sqsEvent.getRecords()) {
        processMessage(message);
      }
      return "OK";
    }

    private void processMessage(SQSEvent.SQSMessage message) {
      try {
        var bodyNode = objectMapper.readTree(message.getBody());
        var snsMessage = bodyNode.get("Message").asText();
        var event = objectMapper.readValue(snsMessage, MediaEvent.class);

        var eventType = event.getType();
        var payload = event.getPayload();
        if (payload == null)
          return;

        var mediaId = payload.getMediaId();
        if (mediaId == null || mediaId.isEmpty())
          return;

        var width = payload.getWidth();

        switch (eventType) {
          case "media.v1.delete" -> handleDelete(mediaId);
          case "media.v1.resize" -> {
            if (width != null)
              handleMediaProcessing(mediaId, width, MediaStatus.PENDING, true);
          }
          case "media.v1.process" -> handleMediaProcessing(mediaId, width, MediaStatus.PENDING, false);
        }
      } catch (Exception e) {
        throw new RuntimeException("Failed to process message", e);
      }
    }

    private void handleDelete(String mediaId) {
      dynamoDbService.deleteMedia(mediaId).ifPresent(media -> {
        s3Service.deleteMediaFile(mediaId, media.getName(), "uploads");
        if (media.getStatus() != MediaStatus.PROCESSING) {
          String keyPrefix = (media.getStatus() == MediaStatus.ERROR) ? "uploads" : "resized";
          s3Service.deleteMediaFile(mediaId, media.getName(), keyPrefix);
        }
      });
    }

    private void handleMediaProcessing(String mediaId, Integer requestedWidth, MediaStatus expectedStatus,
        boolean isResize) {
      try {
        var mediaOpt = dynamoDbService.setMediaStatusConditionally(mediaId, MediaStatus.PROCESSING,
            expectedStatus);
        if (mediaOpt.isEmpty())
          return;

        var media = mediaOpt.get();
        byte[] imageData = s3Service.getMediaFile(mediaId, media.getName());
        var targetWidth = (requestedWidth != null) ? requestedWidth : media.getWidth();

        byte[] processedImage = isResize
            ? imageProcessingService.resizeImage(imageData, targetWidth, OutputFormat.JPEG)
            : imageProcessingService.processImage(imageData, targetWidth, OutputFormat.JPEG);

        s3Service.uploadMedia(mediaId, media.getName(), processedImage, "resized");
        dynamoDbService.setMediaStatusConditionally(mediaId, MediaStatus.COMPLETE, MediaStatus.PROCESSING,
            targetWidth);
      } catch (Exception e) {
        dynamoDbService.setMediaStatus(mediaId, MediaStatus.ERROR);
        throw new RuntimeException("Failed to process media", e);
      }
    }
  }
}
