package com.mediaservice.providers.generation.image;

import com.mediaservice.common.generation.GenerationErrorCode;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.common.generation.provider.JobSpec;
import com.mediaservice.common.generation.provider.ProviderJobId;
import com.mediaservice.common.generation.provider.ProviderKind;
import com.mediaservice.common.generation.provider.ProviderState;
import com.mediaservice.common.generation.provider.ProviderStatus;
import java.awt.Color;
import java.awt.Graphics2D;
import java.awt.RenderingHints;
import java.awt.image.BufferedImage;
import java.io.ByteArrayOutputStream;
import java.security.MessageDigest;
import java.time.Instant;
import java.time.LocalTime;
import java.time.ZoneOffset;
import java.util.HexFormat;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;
import javax.imageio.ImageIO;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.dynamodb.model.AttributeValue;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;
import software.amazon.awssdk.services.dynamodb.model.GetItemRequest;
import software.amazon.awssdk.services.dynamodb.model.PutItemRequest;
import com.mediaservice.providers.generation.core.GenerationProviderException;
import com.mediaservice.providers.generation.core.GenerationRuntimeConfig;

import static com.mediaservice.providers.generation.core.DynamoDbAttrs.n;
import static com.mediaservice.providers.generation.core.DynamoDbAttrs.s;

public class SimulatedGpuProvider implements ImageProvider {
  private static final long CONTROL_CACHE_TTL_MS = 5_000L;

  private final GenerationRuntimeConfig config;
  private final DynamoDbClient dynamoDbClient;
  private final String tableName;

  // In-process cache of the GENERATION#CONTROL/SIMULATOR row. Without it, every sync render
  // does two GetItems (one in generateSync, one inside maybeFail→effectiveFailureRate); under
  // load that's a hot-path tax on what is supposed to be a cheap simulator.
  private volatile SimulatorControl cachedControl;
  private volatile long cachedControlAt;

  public SimulatedGpuProvider(GenerationRuntimeConfig config, DynamoDbClient dynamoDbClient, String tableName) {
    this.config = config;
    this.dynamoDbClient = dynamoDbClient;
    this.tableName = tableName;
  }

  @Override
  public ProviderKind kind() {
    return config.simulatorKind();
  }

  @Override
  public Artifact generateSync(JobSpec spec) {
    SimulatorControl control = simulatorControl();
    if (control.paused()) {
      throw new GenerationProviderException(GenerationErrorCode.SIMULATOR_PAUSED, "Simulated generation provider is paused");
    }
    sleep(config.simulatorColdStartMs() + control.meanDurationMs());
    maybeFail(spec);
    return new Artifact(renderPng(spec), "image/png", ".png", Map.of(
        "provider", "simulated",
        "region", config.region(),
        "model", spec.model() != null ? spec.model() : config.model(),
        "watermark", "simulated-ai-generated",
        "content_safety", "simulated-output-safe",
        "visible_watermark", "simulated-stripes"));
  }

  @Override
  public ProviderJobId submitAsync(JobSpec spec) {
    String providerJobId = "sim_" + UUID.nameUUIDFromBytes((spec.jobId() + ":" + spec.prompt()).getBytes());
    long now = Instant.now().toEpochMilli();
    SimulatorControl control = simulatorControl();
    long completesAt = now + config.simulatorColdStartMs() + control.meanDurationMs();
    Map<String, AttributeValue> state = new HashMap<>();
    state.put("PK", s(StorageConstants.buildSimPk(providerJobId)));
    state.put("SK", s(StorageConstants.DYNAMO_SK_SIM_STATE));
    state.put("providerJobId", s(providerJobId));
    state.put("jobId", s(spec.jobId()));
    state.put("mediaId", s(spec.mediaId() != null ? spec.mediaId() : ""));
    state.put("tenantId", s(spec.tenantId() != null ? spec.tenantId() : ""));
    state.put("prompt", s(spec.prompt() != null ? spec.prompt() : ""));
    state.put("model", s(spec.model() != null ? spec.model() : config.model()));
    state.put("resolution", s(spec.resolution() != null ? spec.resolution() : "512x512"));
    state.put("seed", n(spec.seed() != null ? spec.seed() : 0L));
    state.put("submittedAt", n(now));
    state.put("meanDurationMs", n(control.meanDurationMs()));
    state.put("coldStartMs", n(config.simulatorColdStartMs()));
    state.put("failureSeed", s(hash(spec.jobId() + ":" + spec.prompt())));
    state.put("completesAt", n(completesAt));
    state.put("expiresAt", n(Instant.now().plusSeconds(86400).getEpochSecond()));
    try {
      dynamoDbClient.putItem(PutItemRequest.builder()
          .tableName(tableName)
          .item(state)
          .conditionExpression("attribute_not_exists(PK)")
          .build());
    } catch (ConditionalCheckFailedException ignored) {
      // Another worker (or a redelivered SQS message for the same job) already
      // wrote the simulator state row. The deterministic providerJobId means the
      // existing row already corresponds to this submission; safely fall through.
    }
    return new ProviderJobId(providerJobId);
  }

  @Override
  public ProviderState poll(ProviderJobId providerJobId) {
    var item = dynamoDbClient.getItem(GetItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(StorageConstants.buildSimPk(providerJobId.value())), "SK", s(StorageConstants.DYNAMO_SK_SIM_STATE)))
        .build()).item();
    if (item == null || item.isEmpty()) {
      return new ProviderState(ProviderStatus.FAILED, "Simulator job state not found");
    }
    long completesAt = Long.parseLong(item.get("completesAt").n());
    if (Instant.now().toEpochMilli() < completesAt) {
      return new ProviderState(ProviderStatus.RUNNING, "Simulator job is still running");
    }
    return new ProviderState(ProviderStatus.COMPLETED, "Simulator job complete");
  }

  @Override
  public Artifact fetch(ProviderJobId providerJobId) {
    var item = dynamoDbClient.getItem(GetItemRequest.builder()
        .tableName(tableName)
        .key(Map.of("PK", s(StorageConstants.buildSimPk(providerJobId.value())), "SK", s(StorageConstants.DYNAMO_SK_SIM_STATE)))
        .build()).item();
    if (item == null || item.isEmpty()) {
      throw new GenerationProviderException(GenerationErrorCode.PROVIDER_JOB_NOT_FOUND, "Simulator job state not found");
    }
    String jobId = item.get("jobId").s();
    Long seed = item.containsKey("seed") ? Long.parseLong(item.get("seed").n()) : null;
    return generateSync(new JobSpec(jobId, item.get("mediaId").s(), item.get("tenantId").s(),
        com.mediaservice.common.generation.GenerationOutputType.IMAGE,
        item.get("prompt").s(), item.get("model").s(), item.get("resolution").s(), seed, Map.of()));
  }

  private void maybeFail(JobSpec spec) {
    double failureRate = effectiveFailureRate();
    if (failureRate <= 0.0) {
      return;
    }
    double bucket = (Math.abs(hash(spec.jobId() + spec.prompt()).hashCode()) % 10_000) / 10_000.0;
    if (bucket < failureRate) {
      throw new GenerationProviderException(GenerationErrorCode.SIMULATED_PROVIDER_FAILURE, "Simulated provider failure");
    }
  }

  private double effectiveFailureRate() {
    SimulatorControl control = simulatorControl();
    if (control.failureRate() != null) {
      return control.failureRate();
    }
    if (!config.simulatorChaosBusinessHoursEnabled()) {
      return config.simulatorFailureRate();
    }
    int hour = LocalTime.now(ZoneOffset.UTC).getHour();
    int start = Math.floorMod(config.simulatorChaosStartHourUtc(), 24);
    int end = Math.floorMod(config.simulatorChaosEndHourUtc(), 24);
    boolean inWindow = start <= end
        ? hour >= start && hour < end
        : hour >= start || hour < end;
    return inWindow
        ? Math.max(config.simulatorFailureRate(), config.simulatorChaosFailureRate())
        : config.simulatorFailureRate();
  }

  private SimulatorControl simulatorControl() {
    long now = System.currentTimeMillis();
    SimulatorControl cached = this.cachedControl;
    if (cached != null && (now - cachedControlAt) < CONTROL_CACHE_TTL_MS) {
      return cached;
    }
    SimulatorControl fresh = loadSimulatorControl();
    this.cachedControl = fresh;
    this.cachedControlAt = now;
    return fresh;
  }

  private SimulatorControl loadSimulatorControl() {
    if (dynamoDbClient == null || tableName == null || tableName.isBlank()) {
      return new SimulatorControl(false, config.simulatorMeanDurationMs(), null);
    }
    try {
      var item = dynamoDbClient.getItem(GetItemRequest.builder()
          .tableName(tableName)
          .key(Map.of("PK", s(StorageConstants.DYNAMO_PK_GENERATION_CONTROL), "SK", s(StorageConstants.DYNAMO_SK_GEN_CONTROL_SIMULATOR)))
          .build()).item();
      if (item == null || item.isEmpty()) {
        return new SimulatorControl(false, config.simulatorMeanDurationMs(), null);
      }
      boolean paused = item.containsKey("paused") && Boolean.TRUE.equals(item.get("paused").bool());
      long meanDurationMs = item.containsKey("meanDurationMs") && item.get("meanDurationMs").n() != null
          ? Long.parseLong(item.get("meanDurationMs").n())
          : config.simulatorMeanDurationMs();
      Double failureRate = item.containsKey("failureRate") && item.get("failureRate").n() != null
          ? Double.parseDouble(item.get("failureRate").n())
          : null;
      return new SimulatorControl(paused, meanDurationMs, failureRate);
    } catch (RuntimeException ignored) {
      return new SimulatorControl(false, config.simulatorMeanDurationMs(), null);
    }
  }

  private byte[] renderPng(JobSpec spec) {
    try {
      int width = parseWidth(spec.resolution());
      int height = parseHeight(spec.resolution());
      BufferedImage image = new BufferedImage(width, height, BufferedImage.TYPE_INT_RGB);
      Graphics2D g = image.createGraphics();
      g.setRenderingHint(RenderingHints.KEY_ANTIALIASING, RenderingHints.VALUE_ANTIALIAS_ON);
      String hash = hash(spec.prompt() + ":" + spec.seed());
      Color bg = new Color(Integer.parseInt(hash.substring(0, 2), 16), Integer.parseInt(hash.substring(2, 4), 16),
          Integer.parseInt(hash.substring(4, 6), 16));
      Color accent = new Color(255 - bg.getRed(), 255 - bg.getGreen(), 255 - bg.getBlue());
      g.setColor(bg);
      g.fillRect(0, 0, width, height);
      g.setColor(accent);
      g.fillOval(width / 8, height / 8, width * 3 / 4, height * 3 / 4);
      drawFingerprint(g, hash, width, height);
      drawWatermarkStripes(g, width, height);
      g.dispose();
      ByteArrayOutputStream out = new ByteArrayOutputStream();
      ImageIO.write(image, "png", out);
      return out.toByteArray();
    } catch (Exception e) {
      throw new GenerationProviderException(GenerationErrorCode.SIMULATOR_RENDER_FAILED, e.getMessage());
    }
  }

  private void drawFingerprint(Graphics2D g, String hash, int width, int height) {
    int cell = Math.max(6, width / 80);
    int cols = 12;
    int rows = 4;
    int x = Math.max(16, width / 14);
    int y = height - (rows * cell) - Math.max(16, height / 14);
    g.setColor(new Color(255, 255, 255, 72));
    g.fillRect(x - cell, y - cell, (cols + 2) * cell, (rows + 2) * cell);
    for (int row = 0; row < rows; row++) {
      for (int col = 0; col < cols; col++) {
        int hashIndex = (row * cols + col) % hash.length();
        int nibble = Character.digit(hash.charAt(hashIndex), 16);
        int alpha = (nibble % 2 == 0) ? 90 : 210;
        g.setColor(new Color(255, 255, 255, alpha));
        g.fillRect(x + (col * cell), y + (row * cell), cell - 1, cell - 1);
      }
    }
  }

  private void drawWatermarkStripes(Graphics2D g, int width, int height) {
    int step = Math.max(36, width / 10);
    g.setColor(new Color(255, 255, 255, 36));
    for (int offset = -height; offset < width; offset += step) {
      g.fillPolygon(
          new int[] {offset, offset + step / 5, offset + height + step / 5, offset + height},
          new int[] {0, 0, height, height},
          4);
    }
  }

  private int parseWidth(String resolution) {
    return parseResolutionPart(resolution, 0);
  }

  private int parseHeight(String resolution) {
    return parseResolutionPart(resolution, 1);
  }

  private int parseResolutionPart(String resolution, int index) {
    if (resolution == null || !resolution.contains("x")) {
      return 512;
    }
    try {
      int value = Integer.parseInt(resolution.toLowerCase().split("x")[index].trim());
      return Math.max(128, Math.min(1024, value));
    } catch (Exception e) {
      return 512;
    }
  }

  private String hash(String value) {
    try {
      MessageDigest digest = MessageDigest.getInstance("SHA-256");
      return HexFormat.of().formatHex(digest.digest(value.getBytes()));
    } catch (Exception e) {
      throw new GenerationProviderException(GenerationErrorCode.HASH_FAILED, e.getMessage());
    }
  }

  private void sleep(long millis) {
    if (millis <= 0) {
      return;
    }
    try {
      Thread.sleep(Math.min(millis, 2_000));
    } catch (InterruptedException e) {
      Thread.currentThread().interrupt();
      throw new GenerationProviderException(GenerationErrorCode.INTERRUPTED, "Simulator interrupted");
    }
  }

  private record SimulatorControl(boolean paused, long meanDurationMs, Double failureRate) {
  }
}
