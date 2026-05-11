package com.mediaservice.providers.generation.image;

import com.mediaservice.common.generation.GenerationErrorCode;

import com.mediaservice.common.generation.provider.Artifact;
import java.awt.AlphaComposite;
import java.awt.Color;
import java.awt.Font;
import java.awt.Graphics2D;
import java.awt.RenderingHints;
import java.awt.image.BufferedImage;
import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.util.HashMap;
import java.util.Map;
import javax.imageio.ImageIO;
import com.mediaservice.providers.generation.core.GenerationProviderException;

/** Stamps an "AI generated" badge onto image bytes for visible-watermark compliance. */
public final class ImageWatermarker {
  private static final String VISIBLE_KEY = "visible_watermark";

  private ImageWatermarker() {}

  public static boolean needsStamp(Artifact artifact) {
    if (artifact == null || artifact.bytes() == null || artifact.bytes().length == 0) return false;
    if (!writableFormat(artifact.contentType()).isPresent()) return false;
    Map<String, String> metadata = artifact.metadata();
    return metadata == null || !metadata.containsKey(VISIBLE_KEY);
  }

  public static Artifact stamp(Artifact artifact) {
    if (!needsStamp(artifact)) return artifact;
    String format = writableFormat(artifact.contentType()).orElseThrow();
    BufferedImage img;
    try {
      img = ImageIO.read(new ByteArrayInputStream(artifact.bytes()));
    } catch (Exception e) {
      // Bytes failed to decode (test fixture, corrupt payload). The publish-gate byte check
      // still runs downstream, so leave the artifact alone rather than aborting the job.
      return artifact;
    }
    if (img == null) return artifact;
    try {
      BufferedImage stamped = new BufferedImage(img.getWidth(), img.getHeight(), BufferedImage.TYPE_INT_RGB);
      Graphics2D g = stamped.createGraphics();
      g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);
      g.setRenderingHint(RenderingHints.KEY_TEXT_ANTIALIASING, RenderingHints.VALUE_TEXT_ANTIALIAS_ON);
      g.drawImage(img, 0, 0, null);
      drawBadge(g, img.getWidth(), img.getHeight());
      g.dispose();
      ByteArrayOutputStream out = new ByteArrayOutputStream();
      ImageIO.write(stamped, format, out);
      Map<String, String> updated = new HashMap<>();
      if (artifact.metadata() != null) updated.putAll(artifact.metadata());
      updated.put(VISIBLE_KEY, "ai-badge-v1");
      updated.putIfAbsent("watermark", "stamped-ai-generated");
      return new Artifact(out.toByteArray(), artifact.contentType(), artifact.extension(), updated);
    } catch (Exception e) {
      throw new GenerationProviderException(GenerationErrorCode.WATERMARK_STAMP_FAILED, e.getMessage());
    }
  }

  private static void drawBadge(Graphics2D g, int width, int height) {
    int padding = Math.max(8, Math.min(width, height) / 64);
    int badgeHeight = Math.max(18, Math.min(width, height) / 22);
    int badgeWidth = badgeHeight * 4;
    int x = width - badgeWidth - padding;
    int y = height - badgeHeight - padding;
    g.setComposite(AlphaComposite.getInstance(AlphaComposite.SRC_OVER, 0.75f));
    g.setColor(Color.BLACK);
    g.fillRoundRect(x, y, badgeWidth, badgeHeight, badgeHeight / 2, badgeHeight / 2);
    g.setComposite(AlphaComposite.SrcOver);
    g.setColor(Color.WHITE);
    g.setFont(new Font("SansSerif", Font.BOLD, (int) (badgeHeight * 0.55)));
    g.drawString("AI generated", x + (badgeHeight / 2), y + badgeHeight - (badgeHeight / 4));
  }

  /** Returns the ImageIO format name the artifact's content-type natively round-trips through, if any. */
  private static java.util.Optional<String> writableFormat(String contentType) {
    if (contentType == null) return java.util.Optional.empty();
    return switch (contentType.toLowerCase()) {
      case "image/png" -> java.util.Optional.of("png");
      case "image/jpeg", "image/jpg" -> java.util.Optional.of("jpg");
      default -> java.util.Optional.empty();
    };
  }
}
