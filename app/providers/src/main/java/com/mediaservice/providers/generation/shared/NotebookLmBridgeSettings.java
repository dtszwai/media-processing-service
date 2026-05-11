package com.mediaservice.providers.generation.shared;

import java.time.Duration;
import java.util.Locale;
import java.util.Map;
import com.mediaservice.providers.generation.audio.NotebookLmAudioOverviewProvider;
import com.mediaservice.providers.generation.core.GenerationRuntimeConfig;
import com.mediaservice.providers.generation.core.NotConfiguredException;

/**
 * Configuration for the NotebookLM shell-out provider. Read from environment
 * (or an injected map for tests). Kept separate from {@link GenerationRuntimeConfig}
 * so the main runtime record stays focused on cross-provider concerns; the
 * NotebookLM integration is intentionally niche and may evolve independently
 * as the upstream {@code notebooklm-py} surface shifts.
 *
 * <p>The provider runs {@code <python> <script> --prompt ... --out ...} per
 * job and reads bytes back from the output file; everything below is a knob
 * on that invocation.
 */
public record NotebookLmBridgeSettings(
    String pythonExecutable,
    String scriptPath,
    String storageStatePath,
    String audioFormat,
    String audioLength,
    String language,
    String authUser,
    String instructions,
    Duration runTimeout,
    int pollIntervalSeconds,
    boolean cleanupNotebook
) {
  public static NotebookLmBridgeSettings fromEnvironment(Map<String, String> env) {
    return new NotebookLmBridgeSettings(
        first(env, "NOTEBOOKLM_PYTHON", "python3"),
        first(env, "NOTEBOOKLM_SCRIPT_PATH", "scripts/notebooklm/overview.py"),
        blankToNull(env.get("NOTEBOOKLM_STORAGE_STATE_PATH")),
        first(env, "NOTEBOOKLM_AUDIO_FORMAT", "deep_dive").toLowerCase(Locale.ROOT),
        first(env, "NOTEBOOKLM_AUDIO_LENGTH", "default").toLowerCase(Locale.ROOT),
        first(env, "NOTEBOOKLM_LANGUAGE", "en"),
        first(env, "NOTEBOOKLM_AUTHUSER", ""),
        env.getOrDefault("NOTEBOOKLM_INSTRUCTIONS", ""),
        Duration.ofSeconds(parseLong(env.get("NOTEBOOKLM_TIMEOUT_SECONDS"), 600L)),
        (int) parseLong(env.get("NOTEBOOKLM_POLL_INTERVAL_SECONDS"), 5L),
        parseBoolean(env.get("NOTEBOOKLM_CLEANUP_NOTEBOOK"), true));
  }

  public void requireConfigured() {
    if (storageStatePath == null || storageStatePath.isBlank()) {
      throw new NotConfiguredException(
          "NotebookLmAudioOverviewProvider",
          "NOTEBOOKLM_STORAGE_STATE_PATH (run scripts/notebooklm/login.py to capture one)");
    }
  }

  private static String first(Map<String, String> env, String key, String fallback) {
    String value = env.get(key);
    return value == null || value.isBlank() ? fallback : value;
  }

  private static String blankToNull(String value) {
    return value == null || value.isBlank() ? null : value;
  }

  private static long parseLong(String value, long fallback) {
    if (value == null || value.isBlank()) {
      return fallback;
    }
    try {
      return Long.parseLong(value);
    } catch (NumberFormatException e) {
      return fallback;
    }
  }

  private static boolean parseBoolean(String value, boolean fallback) {
    if (value == null || value.isBlank()) {
      return fallback;
    }
    return Boolean.parseBoolean(value);
  }
}
