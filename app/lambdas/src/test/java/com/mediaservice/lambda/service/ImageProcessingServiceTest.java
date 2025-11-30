package com.mediaservice.lambda.service;

import com.mediaservice.lambda.model.OutputFormat;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Nested;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.params.ParameterizedTest;
import org.junit.jupiter.params.provider.ValueSource;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;

import static org.assertj.core.api.Assertions.assertThat;

class ImageProcessingServiceTest {
    private ImageProcessingService service;

    @BeforeEach
    void setUp() {
        service = new ImageProcessingService();
    }

    @Nested
    @DisplayName("processImage")
    class ProcessImage {
        @Test
        @DisplayName("should resize image to target width")
        void shouldResizeToTargetWidth() throws IOException {
            byte[] inputImage = createTestImage(1000, 800);
            byte[] result = service.processImage(inputImage, 500, OutputFormat.JPEG);
            var outputImage = ImageIO.read(new ByteArrayInputStream(result));
            assertThat(outputImage.getWidth()).isEqualTo(500);
        }

        @Test
        @DisplayName("should use default width when null")
        void shouldUseDefaultWidthWhenNull() throws IOException {
            byte[] inputImage = createTestImage(1000, 800);
            byte[] result = service.processImage(inputImage, null, OutputFormat.JPEG);
            var outputImage = ImageIO.read(new ByteArrayInputStream(result));
            assertThat(outputImage.getWidth()).isEqualTo(500); // default width
        }

        @Test
        @DisplayName("should use default width when zero")
        void shouldUseDefaultWidthWhenZero() throws IOException {
            byte[] inputImage = createTestImage(1000, 800);
            byte[] result = service.processImage(inputImage, 0, OutputFormat.JPEG);
            var outputImage = ImageIO.read(new ByteArrayInputStream(result));
            assertThat(outputImage.getWidth()).isEqualTo(500);
        }

        @ParameterizedTest
        @ValueSource(ints = { 100, 300, 500, 800, 1024 })
        @DisplayName("should handle various target widths")
        void shouldHandleVariousWidths(int targetWidth) throws IOException {
            byte[] inputImage = createTestImage(2000, 1600);
            byte[] result = service.processImage(inputImage, targetWidth, OutputFormat.JPEG);
            var outputImage = ImageIO.read(new ByteArrayInputStream(result));
            assertThat(outputImage.getWidth()).isEqualTo(targetWidth);
        }

        @Test
        @DisplayName("should output JPEG format")
        void shouldOutputJpegFormat() throws IOException {
            byte[] inputImage = createTestImage(500, 400);
            byte[] result = service.processImage(inputImage, 300, OutputFormat.JPEG);
            // JPEG files start with FFD8
            assertThat(result[0] & 0xFF).isEqualTo(0xFF);
            assertThat(result[1] & 0xFF).isEqualTo(0xD8);
        }

        @Test
        @DisplayName("should output PNG format")
        void shouldOutputPngFormat() throws IOException {
            byte[] inputImage = createTestImage(500, 400);
            byte[] result = service.processImage(inputImage, 300, OutputFormat.PNG);
            // PNG files start with 89 50 4E 47 (0x89 'PNG')
            assertThat(result[0] & 0xFF).isEqualTo(0x89);
            assertThat(result[1] & 0xFF).isEqualTo(0x50); // 'P'
            assertThat(result[2] & 0xFF).isEqualTo(0x4E); // 'N'
            assertThat(result[3] & 0xFF).isEqualTo(0x47); // 'G'
        }

        @Test
        @DisplayName("should maintain aspect ratio")
        void shouldMaintainAspectRatio() throws IOException {
            byte[] inputImage = createTestImage(1000, 500); // 2:1 ratio
            byte[] result = service.processImage(inputImage, 500, OutputFormat.JPEG);
            var outputImage = ImageIO.read(new ByteArrayInputStream(result));
            assertThat(outputImage.getWidth()).isEqualTo(500);
            assertThat(outputImage.getHeight()).isEqualTo(250); // maintains 2:1 ratio
        }
    }

    @Nested
    @DisplayName("resizeImage")
    class ResizeImage {
        @Test
        @DisplayName("should resize image with different watermark position")
        void shouldResizeWithWatermark() throws IOException {
            byte[] inputImage = createTestImage(1000, 800);
            byte[] result = service.resizeImage(inputImage, 600, OutputFormat.JPEG);
            var outputImage = ImageIO.read(new ByteArrayInputStream(result));
            assertThat(outputImage.getWidth()).isEqualTo(600);
        }
    }

    private byte[] createTestImage(int width, int height) throws IOException {
        var image = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
        // Fill with a gradient for visual testing
        for (int x = 0; x < width; x++) {
            for (int y = 0; y < height; y++) {
                int rgb = ((x * 255 / width) << 16) | ((y * 255 / height) << 8) | 128;
                image.setRGB(x, y, rgb);
            }
        }
        var baos = new ByteArrayOutputStream();
        ImageIO.write(image, "png", baos);
        return baos.toByteArray();
    }
}
