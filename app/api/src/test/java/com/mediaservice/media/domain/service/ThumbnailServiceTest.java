package com.mediaservice.media.domain.service;

import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.DisplayName;
import org.junit.jupiter.api.Test;

import javax.imageio.ImageIO;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;

import static org.assertj.core.api.Assertions.assertThat;

class ThumbnailServiceTest {

    private ThumbnailService thumbnailService;

    @BeforeEach
    void setUp() {
        thumbnailService = new ThumbnailService();
    }

    @Test
    @DisplayName("generates 200px wide JPEG thumbnail from larger image")
    void generatesCorrectSizeThumbnail() throws IOException {
        byte[] original = createTestImage(1920, 1080);

        byte[] result = thumbnailService.generate(original);

        assertThat(result).isNotEmpty();
        BufferedImage img = ImageIO.read(new ByteArrayInputStream(result));
        assertThat(img.getWidth()).isEqualTo(200);
        assertThat(result.length).isLessThan(original.length);
    }

    @Test
    @DisplayName("does not upscale images smaller than 200px")
    void doesNotUpscaleSmallImages() throws IOException {
        byte[] original = createTestImage(100, 80);

        byte[] result = thumbnailService.generate(original);

        BufferedImage img = ImageIO.read(new ByteArrayInputStream(result));
        assertThat(img.getWidth()).isLessThanOrEqualTo(100);
    }

    @Test
    @DisplayName("output is JPEG format")
    void outputIsJpeg() throws IOException {
        byte[] original = createTestImage(500, 400);

        byte[] result = thumbnailService.generate(original);

        // JPEG magic bytes: FF D8
        assertThat(result[0] & 0xFF).isEqualTo(0xFF);
        assertThat(result[1] & 0xFF).isEqualTo(0xD8);
    }

    private byte[] createTestImage(int width, int height) throws IOException {
        var image = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
        for (int x = 0; x < width; x++) {
            for (int y = 0; y < height; y++) {
                image.setRGB(x, y, ((x * 255 / width) << 16) | ((y * 255 / height) << 8) | 128);
            }
        }
        var baos = new ByteArrayOutputStream();
        ImageIO.write(image, "png", baos);
        return baos.toByteArray();
    }
}
