package com.mediaservice.providers.generation.audio;

import com.mediaservice.common.generation.GenerationErrorCode;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.providers.generation.audio.AudioOverviewProvider;
import com.mediaservice.common.generation.provider.JobSpec;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Objects;
import com.mediaservice.providers.generation.core.GenerationProviderException;
import com.mediaservice.providers.generation.shared.NotebookLmBridgeSettings;
import com.mediaservice.providers.generation.shared.ProcessRunner;

/**
 * NotebookLM audio overview provider.
 *
 * <p>Google does not expose a documented NotebookLM API for personal Google
 * accounts; only NotebookLM Enterprise (Discovery Engine, paid Gemini
 * Enterprise SKU) has a public surface. The only realistic programmatic path
 * for a personal account is the community-maintained {@code notebooklm-py}
 * library, which speaks NotebookLM's private HTTP API using cookies captured
 * from a real browser login.
 *
 * <p>This provider shells out to {@code scripts/notebooklm/overview.py} (which
 * wraps {@code notebooklm-py}). The script writes the audio bytes to a temp
 * file and emits a one-line JSON summary on stdout; this class reads the file
 * back and returns the bytes as a generation {@link Artifact}.
 *
 * <p>Caveats:
 * <ul>
 *   <li>{@code notebooklm-py} speaks an undocumented Google API and may break
 *       without notice when Google rotates internal endpoints.</li>
 *   <li>The captured Google session in {@code storage_state.json} expires
 *       periodically; refresh by re-running {@code scripts/notebooklm/login.py}.</li>
 *   <li>Automating a personal Google account is TOS-grey; intended for the
 *       repo's architecture-showcase posture, not for end-user-facing traffic.</li>
 * </ul>
 */
public class NotebookLmAudioOverviewProvider implements AudioOverviewProvider {
  private static final ObjectMapper MAPPER = new ObjectMapper();
  private static final int STDERR_TRUNCATE_LIMIT = 1024;
  private static final Duration PROBE_CACHE_TTL = Duration.ofSeconds(60);
  private static final Duration PROBE_TIMEOUT = Duration.ofSeconds(20);

  private final NotebookLmBridgeSettings settings;
  private final ProcessRunner processRunner;

  // Probe results are cached in-process so submit-time pre-flight does not hammer Google.
  // A volatile snapshot is fine — readers tolerate a one-shot stale OK; the worst case is a
  // single job runs through ENCODE before the cache rolls over and fail-fast kicks in.
  private volatile AuthHealth cachedProbe;
  private volatile Instant cachedProbeAt;

  public NotebookLmAudioOverviewProvider(NotebookLmBridgeSettings settings) {
    this(settings, ProcessRunner.systemDefault());
  }

  public NotebookLmAudioOverviewProvider(NotebookLmBridgeSettings settings, ProcessRunner processRunner) {
    this.settings = Objects.requireNonNull(settings, "settings");
    this.processRunner = Objects.requireNonNull(processRunner, "processRunner");
  }

  @Override
  public AuthHealth health() {
    if (settings.storageStatePath() == null || settings.storageStatePath().isBlank()) {
      return AuthHealth.AUTH_MISSING;
    }
    if (!Files.exists(Paths.get(settings.storageStatePath()))) {
      return AuthHealth.AUTH_MISSING;
    }
    AuthHealth cached = cachedProbe;
    Instant at = cachedProbeAt;
    if (cached != null && at != null && Duration.between(at, Instant.now()).compareTo(PROBE_CACHE_TTL) < 0) {
      return cached;
    }
    AuthHealth fresh = runProbe();
    cachedProbe = fresh;
    cachedProbeAt = Instant.now();
    return fresh;
  }

  private AuthHealth runProbe() {
    List<String> cmd = new ArrayList<>();
    cmd.add(settings.pythonExecutable());
    cmd.add(settings.scriptPath());
    cmd.add("--probe");
    cmd.add("--storage-state"); cmd.add(settings.storageStatePath());
    if (settings.authUser() != null && !settings.authUser().isBlank()) {
      cmd.add("--authuser"); cmd.add(settings.authUser());
    }
    try {
      ProcessRunner.Result result = processRunner.run(cmd, PROBE_TIMEOUT);
      return result.exitCode() == 0 ? AuthHealth.OK : AuthHealth.AUTH_EXPIRED;
    } catch (IOException | RuntimeException e) {
      // Probe is best-effort. Treat infrastructure failure (python missing, timeout) as expired so
      // submit() rejects fast — better UX than blowing up downstream stages with the same root cause.
      return AuthHealth.AUTH_EXPIRED;
    }
  }

  @Override
  public Artifact generateOverview(JobSpec spec) {
    settings.requireConfigured();
    if (spec == null || spec.prompt() == null || spec.prompt().isBlank()) {
      throw new GenerationProviderException(GenerationErrorCode.INVALID_JOB_SPEC,
          "NotebookLM requires a non-blank prompt");
    }
    Path output = null;
    try {
      output = Files.createTempFile("notebooklm-" + spec.jobId() + "-", ".m4a");
      List<String> command = buildCommand(spec, output);
      ProcessRunner.Result result = processRunner.run(command, settings.runTimeout());
      if (result.exitCode() != 0) {
        String code = classifyExit(result.exitCode(), result.stderr());
        if ("NOTEBOOKLM_AUTH_EXPIRED".equals(code)) {
          // Invalidate cached OK so the next submit() probes again instead of waving traffic through.
          cachedProbe = AuthHealth.AUTH_EXPIRED;
          cachedProbeAt = Instant.now();
        }
        throw new GenerationProviderException(
            code,
            "NotebookLM bridge failed (exit " + result.exitCode() + "): " + sanitizeStderr(result.stderr()));
      }
      byte[] bytes = Files.readAllBytes(output);
      if (bytes.length == 0) {
        throw new GenerationProviderException(GenerationErrorCode.NOTEBOOKLM_EMPTY_ARTIFACT,
            "NotebookLM bridge returned an empty audio file");
      }
      AudioPayload payload = normalizeAudioPayload(bytes);
      return new Artifact(payload.bytes(), payload.contentType(), payload.extension(), buildMetadata(result.stdout()));
    } catch (IOException e) {
      throw new GenerationProviderException(GenerationErrorCode.NOTEBOOKLM_IO_FAILED, e.getMessage());
    } finally {
      if (output != null) {
        try {
          Files.deleteIfExists(output);
        } catch (IOException ignored) {
        }
      }
    }
  }

  private List<String> buildCommand(JobSpec spec, Path outputPath) {
    List<String> cmd = new ArrayList<>();
    cmd.add(settings.pythonExecutable());
    cmd.add(settings.scriptPath());
    cmd.add("--prompt"); cmd.add(spec.prompt());
    cmd.add("--out"); cmd.add(outputPath.toAbsolutePath().toString());
    cmd.add("--storage-state"); cmd.add(settings.storageStatePath());
    cmd.add("--audio-format"); cmd.add(settings.audioFormat());
    cmd.add("--audio-length"); cmd.add(settings.audioLength());
    cmd.add("--language"); cmd.add(settings.language());
    if (settings.authUser() != null && !settings.authUser().isBlank()) {
      cmd.add("--authuser"); cmd.add(settings.authUser());
    }
    cmd.add("--timeout"); cmd.add(Long.toString(settings.runTimeout().toSeconds()));
    cmd.add("--poll-interval"); cmd.add(Integer.toString(settings.pollIntervalSeconds()));
    cmd.add("--source-title"); cmd.add("Prompt for job " + spec.jobId());
    cmd.add("--notebook-title"); cmd.add("audio-overview-" + spec.jobId());
    if (settings.cleanupNotebook()) {
      cmd.add("--cleanup-notebook");
    }
    String instructions = settings.instructions();
    if (instructions != null && !instructions.isBlank()) {
      cmd.add("--instructions"); cmd.add(instructions);
    }
    return cmd;
  }

  private Map<String, String> buildMetadata(String stdout) {
    Map<String, String> metadata = new HashMap<>();
    metadata.put("provider", "notebooklm");
    metadata.put("is_ai_generated", "true");
    metadata.put("disclosure", "AI-generated audio overview via NotebookLM");
    metadata.put("audio_format", settings.audioFormat());
    metadata.put("audio_length", settings.audioLength());
    metadata.put("language", settings.language());
    if (settings.authUser() != null && !settings.authUser().isBlank()) {
      metadata.put("authuser", settings.authUser());
    }
    JsonNode summary = parseSummary(stdout);
    if (summary != null) {
      copyText(summary, "notebook_id", metadata);
      copyText(summary, "task_id", metadata);
      JsonNode elapsed = summary.path("elapsed_ms");
      if (elapsed.isNumber()) {
        metadata.put("elapsed_ms", Long.toString(elapsed.asLong()));
      }
    }
    return Map.copyOf(metadata);
  }

  private JsonNode parseSummary(String stdout) {
    if (stdout == null) {
      return null;
    }
    String lastNonEmpty = null;
    for (String line : stdout.split("\\R")) {
      String trimmed = line.trim();
      if (!trimmed.isEmpty()) {
        lastNonEmpty = trimmed;
      }
    }
    if (lastNonEmpty == null) {
      return null;
    }
    try {
      return MAPPER.readTree(lastNonEmpty);
    } catch (Exception ignored) {
      return null;
    }
  }

  private static void copyText(JsonNode summary, String key, Map<String, String> dest) {
    JsonNode node = summary.path(key);
    if (node.isTextual()) {
      dest.put(key, node.asText());
    }
  }

  static final byte[] DISCLOSURE_MARKER_BYTES =
      "AI-generated audio".getBytes(StandardCharsets.ISO_8859_1);

  public static boolean containsDisclosureMarker(byte[] bytes) {
    if (bytes == null) {
      return false;
    }
    byte[] needle = DISCLOSURE_MARKER_BYTES;
    int limit = bytes.length - needle.length;
    outer:
    for (int i = 0; i <= limit; i++) {
      for (int j = 0; j < needle.length; j++) {
        if (bytes[i + j] != needle[j]) {
          continue outer;
        }
      }
      return true;
    }
    return false;
  }

  private static byte[] withDisclosureMarker(byte[] bytes) {
    if (containsDisclosureMarker(bytes)) {
      return bytes;
    }
    byte[] description = "AI disclosure".getBytes(StandardCharsets.UTF_8);
    byte[] value = "AI-generated audio overview via NotebookLM".getBytes(StandardCharsets.UTF_8);
    byte[] payload = new byte[1 + description.length + 1 + value.length];
    payload[0] = 3; // UTF-8 text encoding for ID3v2.3 text frames.
    System.arraycopy(description, 0, payload, 1, description.length);
    System.arraycopy(value, 0, payload, 1 + description.length + 1, value.length);

    int frameSize = payload.length;
    int tagSize = 10 + frameSize;
    byte[] tagged = new byte[10 + tagSize + bytes.length];
    tagged[0] = 'I';
    tagged[1] = 'D';
    tagged[2] = '3';
    tagged[3] = 3;
    writeSynchsafe(tagged, 6, tagSize);
    tagged[10] = 'T';
    tagged[11] = 'X';
    tagged[12] = 'X';
    tagged[13] = 'X';
    writeInt(tagged, 14, frameSize);
    System.arraycopy(payload, 0, tagged, 20, payload.length);
    System.arraycopy(bytes, 0, tagged, 10 + tagSize, bytes.length);
    return tagged;
  }

  private static AudioPayload normalizeAudioPayload(byte[] bytes) {
    if (looksLikeIsoBaseMedia(bytes)) {
      return new AudioPayload(bytes, "audio/mp4", ".m4a");
    }
    return new AudioPayload(withDisclosureMarker(bytes), "audio/mpeg", ".mp3");
  }

  private static boolean looksLikeIsoBaseMedia(byte[] bytes) {
    return bytes.length >= 12
        && bytes[4] == 'f'
        && bytes[5] == 't'
        && bytes[6] == 'y'
        && bytes[7] == 'p';
  }

  private static void writeSynchsafe(byte[] target, int offset, int value) {
    target[offset] = (byte) ((value >> 21) & 0x7F);
    target[offset + 1] = (byte) ((value >> 14) & 0x7F);
    target[offset + 2] = (byte) ((value >> 7) & 0x7F);
    target[offset + 3] = (byte) (value & 0x7F);
  }

  private static void writeInt(byte[] target, int offset, int value) {
    target[offset] = (byte) ((value >> 24) & 0xFF);
    target[offset + 1] = (byte) ((value >> 16) & 0xFF);
    target[offset + 2] = (byte) ((value >> 8) & 0xFF);
    target[offset + 3] = (byte) (value & 0xFF);
  }

  private record AudioPayload(byte[] bytes, String contentType, String extension) {
  }

  private static String classifyExit(int exitCode, String stderr) {
    return switch (exitCode) {
      case 1 -> "NOTEBOOKLM_NOT_CONFIGURED";
      case 2 -> isAuthExpiredStderr(stderr) ? "NOTEBOOKLM_AUTH_EXPIRED" : "NOTEBOOKLM_RPC_FAILED";
      default -> "NOTEBOOKLM_BRIDGE_FAILED";
    };
  }

  static boolean isAuthExpiredStderr(String stderr) {
    if (stderr == null) {
      return false;
    }
    String lower = stderr.toLowerCase(Locale.ROOT);
    // The python bridge surfaces several auth-failure phrasings depending on whether the
    // session storage is unreadable, the cookies were rejected by NotebookLM, or the
    // request was redirected to an accounts.google.com sign-in flow.
    return lower.contains("authentication expired")
        || (lower.contains("authentication") && lower.contains("invalid"))
        || lower.contains("auth redirect")
        || lower.contains("sign in")
        || lower.contains("notebooklm_auth_expired");
  }

  private static String sanitizeStderr(String text) {
    if (text == null) {
      return "";
    }
    String trimmed = text.trim();
    return trimmed.length() > STDERR_TRUNCATE_LIMIT
        ? trimmed.substring(0, STDERR_TRUNCATE_LIMIT) + "..."
        : trimmed;
  }
}
