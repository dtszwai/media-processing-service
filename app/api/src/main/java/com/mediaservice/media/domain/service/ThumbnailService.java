package com.mediaservice.media.domain.service;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import javax.imageio.ImageIO;
import net.coobird.thumbnailator.Thumbnails;
import org.springframework.stereotype.Service;

@Service
public class ThumbnailService {

    private static final int MAX_WIDTH = 200;
    private static final float QUALITY = 0.5f;

    /**
     * Generate a 200px JPEG thumbnail from image bytes.
     * Does not upscale images smaller than 200px.
     */
    public byte[] generate(byte[] imageData) throws IOException {
        var sourceImage = ImageIO.read(new ByteArrayInputStream(imageData));
        int targetWidth = Math.min(MAX_WIDTH, sourceImage.getWidth());

        var outputStream = new ByteArrayOutputStream();
        Thumbnails.of(new ByteArrayInputStream(imageData))
            .width(targetWidth)
            .outputFormat("jpeg")
            .outputQuality(QUALITY)
            .toOutputStream(outputStream);

        return outputStream.toByteArray();
    }
}
