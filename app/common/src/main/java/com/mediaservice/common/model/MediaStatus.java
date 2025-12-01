package com.mediaservice.common.model;

public enum MediaStatus {
  PENDING_UPLOAD,
  PENDING,
  PROCESSING,
  COMPLETE,
  ERROR,
  /** Soft deleted - S3 files removed, record retained for analytics/audit */
  DELETED
}
