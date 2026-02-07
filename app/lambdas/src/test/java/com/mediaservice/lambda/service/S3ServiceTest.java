package com.mediaservice.lambda.service;

import com.mediaservice.common.model.OutputFormat;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.extension.ExtendWith;
import org.mockito.Mock;
import org.mockito.junit.jupiter.MockitoExtension;
import software.amazon.awssdk.core.sync.RequestBody;
import software.amazon.awssdk.services.s3.S3Client;
import software.amazon.awssdk.services.s3.model.PutObjectRequest;

import static org.mockito.ArgumentMatchers.any;
import static org.mockito.ArgumentMatchers.argThat;
import static org.mockito.Mockito.verify;

@ExtendWith(MockitoExtension.class)
class S3ServiceTest {

    @Mock
    private S3Client mockS3Client;

    private S3Service s3Service;

    @BeforeEach
    void setUp() {
        s3Service = new S3Service(mockS3Client, "test-bucket");
    }

    @Nested
    @DisplayName("uploadPreview")
    class UploadPreview {

        @Test
        @DisplayName("should upload preview to correct key with cache headers")
        void uploadPreview_uploadsToCorrectKey() {
            byte[] previewData = "preview-data".getBytes();
            String tenantId = "default";
            String mediaId = "test-media-id";

            s3Service.uploadPreview(tenantId, mediaId, previewData, OutputFormat.JPEG);

            verify(mockS3Client).putObject(argThat((PutObjectRequest req) ->
                req.key().equals(tenantId + "/" + mediaId + "/preview.jpeg") &&
                req.contentType().equals("image/jpeg") &&
                req.cacheControl() != null &&
                req.cacheControl().contains("max-age=31536000")
            ), any(RequestBody.class));
        }

        @Test
        @DisplayName("should handle PNG format")
        void uploadPreview_handlesPngFormat() {
            byte[] previewData = "preview-data".getBytes();
            String tenantId = "default";
            String mediaId = "test-media-id";

            s3Service.uploadPreview(tenantId, mediaId, previewData, OutputFormat.PNG);

            verify(mockS3Client).putObject(argThat((PutObjectRequest req) ->
                req.key().equals(tenantId + "/" + mediaId + "/preview.png") &&
                req.contentType().equals("image/png")
            ), any(RequestBody.class));
        }

        @Test
        @DisplayName("should default to JPEG when null format")
        void uploadPreview_defaultsToJpeg() {
            byte[] previewData = "preview-data".getBytes();
            String tenantId = "default";
            String mediaId = "test-media-id";

            s3Service.uploadPreview(tenantId, mediaId, previewData, null);

            verify(mockS3Client).putObject(argThat((PutObjectRequest req) ->
                req.key().equals(tenantId + "/" + mediaId + "/preview.jpeg")
            ), any(RequestBody.class));
        }
    }
}
