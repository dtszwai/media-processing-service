package com.mediaservice.media.application;

import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.OutputFormat;

/**
 * Result type for download operations.
 * Represents the different states a download request can result in.
 */
public sealed interface DownloadResult {

    /**
     * Download is ready with presigned URL.
     */
    record Ready(String url, Media media) implements DownloadResult {
        public OutputFormat outputFormat() {
            return media.getOutputFormatOrDefault();
        }

        public Integer width() {
            return media.getWidth();
        }
    }

    /**
     * Media is still being processed.
     */
    record Processing(String mediaId) implements DownloadResult {}

    /**
     * Media not found.
     */
    record NotFound() implements DownloadResult {}
}
