package com.mediaservice.shared.http.error;

import com.mediaservice.shared.http.filter.RequestIdFilter;
import io.github.resilience4j.circuitbreaker.CallNotPermittedException;
import lombok.extern.slf4j.Slf4j;
import org.slf4j.MDC;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.http.converter.HttpMessageNotReadableException;
import org.springframework.web.HttpMediaTypeNotSupportedException;
import org.springframework.web.bind.MethodArgumentNotValidException;
import org.springframework.web.bind.annotation.ExceptionHandler;
import org.springframework.web.bind.annotation.RestControllerAdvice;
import org.springframework.web.multipart.MaxUploadSizeExceededException;
import org.springframework.web.servlet.resource.NoResourceFoundException;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;

@Slf4j
@RestControllerAdvice
public class GlobalExceptionHandler {
  private String getRequestId() {
    return MDC.get(RequestIdFilter.MDC_REQUEST_ID);
  }

  @ExceptionHandler(HttpMediaTypeNotSupportedException.class)
  public ResponseEntity<ErrorResponse> handleMediaTypeNotSupported(HttpMediaTypeNotSupportedException e) {
    String message = "Content-Type '" + e.getContentType() + "' is not supported. Use 'application/json'.";
    return ResponseEntity.status(HttpStatus.UNSUPPORTED_MEDIA_TYPE)
        .body(ErrorResponse.builder()
            .message(message)
            .status(415)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(HttpMessageNotReadableException.class)
  public ResponseEntity<ErrorResponse> handleMessageNotReadable(HttpMessageNotReadableException e) {
    return ResponseEntity.badRequest()
        .body(ErrorResponse.builder()
            .message("Invalid request body. Expected valid JSON.")
            .status(400)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(MaxUploadSizeExceededException.class)
  public ResponseEntity<ErrorResponse> handleMaxUploadSizeExceeded(MaxUploadSizeExceededException e) {
    log.warn("File size exceeded: {}", e.getMessage());
    return ResponseEntity.badRequest()
        .body(ErrorResponse.builder()
            .message("Failed to upload media. Check the file size. Max size is 100 MB.")
            .status(400)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(MethodArgumentNotValidException.class)
  public ResponseEntity<ErrorResponse> handleValidationException(MethodArgumentNotValidException e) {
    String message = e.getBindingResult().getFieldErrors().stream()
        .map(error -> error.getField() + ": " + error.getDefaultMessage())
        .findFirst()
        .orElse("Validation failed");

    return ResponseEntity.badRequest()
        .body(ErrorResponse.builder()
            .message(message)
            .status(400)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(IllegalArgumentException.class)
  public ResponseEntity<ErrorResponse> handleIllegalArgument(IllegalArgumentException e) {
    return ResponseEntity.badRequest()
        .body(ErrorResponse.builder()
            .message(e.getMessage())
            .status(400)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(java.io.IOException.class)
  public ResponseEntity<ErrorResponse> handleIOException(java.io.IOException e) {
    log.error("IO error: {}", e.getMessage(), e);
    return ResponseEntity.internalServerError()
        .body(ErrorResponse.builder()
            .message("Internal server error")
            .status(500)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(ConditionalCheckFailedException.class)
  public ResponseEntity<ErrorResponse> handleConditionalCheckFailed(ConditionalCheckFailedException e) {
    log.warn("Conditional check failed: {}", e.getMessage());
    return ResponseEntity.status(HttpStatus.CONFLICT)
        .body(ErrorResponse.builder()
            .message("Resource state conflict")
            .status(409)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(MediaConflictException.class)
  public ResponseEntity<ErrorResponse> handleMediaConflict(MediaConflictException e) {
    return ResponseEntity.status(HttpStatus.CONFLICT)
        .body(ErrorResponse.builder()
            .message(e.getMessage())
            .status(409)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(MediaGoneException.class)
  public ResponseEntity<ErrorResponse> handleMediaGone(MediaGoneException e) {
    log.info("Media resource gone: {}", e.getMessage());
    return ResponseEntity.status(HttpStatus.GONE)
        .body(ErrorResponse.builder()
            .message(e.getMessage())
            .status(410)
            .requestId(getRequestId())
            .deletedAt(e.getDeletedAt())
            .build());
  }

  @ExceptionHandler(InvalidImageException.class)
  public ResponseEntity<ErrorResponse> handleInvalidImage(InvalidImageException e) {
    log.warn("Invalid image upload: {}", e.getMessage());
    return ResponseEntity.status(HttpStatus.UNPROCESSABLE_ENTITY)
        .body(ErrorResponse.builder()
            .message(e.getMessage())
            .status(422)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(CallNotPermittedException.class)
  public ResponseEntity<ErrorResponse> handleCircuitBreakerOpen(CallNotPermittedException e) {
    log.warn("Circuit breaker is open: {}", e.getMessage());
    return ResponseEntity.status(HttpStatus.SERVICE_UNAVAILABLE)
        .body(ErrorResponse.builder()
            .message("Service temporarily unavailable. Please try again later.")
            .status(503)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(NoResourceFoundException.class)
  public ResponseEntity<ErrorResponse> handleNoResourceFound(NoResourceFoundException e) {
    return ResponseEntity.status(HttpStatus.NOT_FOUND)
        .body(ErrorResponse.builder()
            .message("Not found")
            .status(404)
            .requestId(getRequestId())
            .build());
  }

  @ExceptionHandler(Exception.class)
  public ResponseEntity<ErrorResponse> handleGenericException(Exception e) {
    log.error("Unexpected error: {}", e.getMessage(), e);
    return ResponseEntity.internalServerError()
        .body(ErrorResponse.builder()
            .message("Internal server error")
            .status(500)
            .requestId(getRequestId())
            .build());
  }
}
