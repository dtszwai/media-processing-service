package com.mediaservice.providers.generation.audio;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.mediaservice.common.generation.GenerationOutputType;
import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.common.generation.provider.JobSpec;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.time.Duration;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import com.mediaservice.providers.generation.core.GenerationProviderException;
import com.mediaservice.providers.generation.core.NotConfiguredException;
import com.mediaservice.providers.generation.shared.NotebookLmBridgeSettings;
import com.mediaservice.providers.generation.shared.ProcessRunner;

class NotebookLmAudioOverviewProviderTest {

  @Test
  void rejectsWhenStorageStatePathMissing() {
    var env = new HashMap<String, String>();
    var settings = NotebookLmBridgeSettings.fromEnvironment(env);
    var provider = new NotebookLmAudioOverviewProvider(settings, (cmd, t) -> {
      throw new AssertionError("script must not be invoked when storage state is missing");
    });
    assertThatThrownBy(() -> provider.generateOverview(spec("plain")))
        .isInstanceOf(NotConfiguredException.class)
        .hasMessageContaining("NOTEBOOKLM_STORAGE_STATE_PATH");
  }

  @Test
  void rejectsBlankPrompt(@TempDir Path tmp) throws IOException {
    var settings = settingsWith(tmp, Map.of());
    var provider = new NotebookLmAudioOverviewProvider(settings, (cmd, t) -> {
      throw new AssertionError("script must not be invoked when prompt is blank");
    });
    assertThatThrownBy(() -> provider.generateOverview(spec("   ")))
        .isInstanceOf(GenerationProviderException.class)
        .hasMessageContaining("non-blank prompt");
  }

  @Test
  void shellsOutAndReturnsNotebookM4aArtifact(@TempDir Path tmp) throws IOException {
    byte[] mockAudio = fakeM4a();
    var captured = new CapturingRunner(mockAudio, 0,
        "{\"output\":\"/tmp/x.m4a\",\"notebook_id\":\"nb_abc\",\"task_id\":\"t1\",\"elapsed_ms\":42}\n",
        "");
    var settings = settingsWith(tmp, Map.of(
        "NOTEBOOKLM_AUDIO_FORMAT", "brief",
        "NOTEBOOKLM_LANGUAGE", "es",
        "NOTEBOOKLM_AUTHUSER", "1",
        "NOTEBOOKLM_INSTRUCTIONS", "be concise"));

    var provider = new NotebookLmAudioOverviewProvider(settings, captured);
    Artifact artifact = provider.generateOverview(spec("hello world"));

    assertThat(artifact.bytes()).isEqualTo(mockAudio);
    assertThat(artifact.contentType()).isEqualTo("audio/mp4");
    assertThat(artifact.extension()).isEqualTo(".m4a");
    assertThat(artifact.metadata())
        .containsEntry("provider", "notebooklm")
        .containsEntry("is_ai_generated", "true")
        .containsEntry("audio_format", "brief")
        .containsEntry("language", "es")
        .containsEntry("authuser", "1")
        .containsEntry("notebook_id", "nb_abc")
        .containsEntry("task_id", "t1")
        .containsEntry("elapsed_ms", "42")
        .containsKey("disclosure");

    List<String> cmd = captured.lastCommand();
    assertThat(cmd).startsWith("python3", "scripts/notebooklm/overview.py");
    assertThat(cmd).contains("--prompt", "hello world");
    assertThat(cmd).contains("--audio-format", "brief");
    assertThat(cmd).contains("--language", "es");
    assertThat(cmd).contains("--authuser", "1");
    assertThat(cmd).contains("--instructions", "be concise");
    assertThat(cmd).contains("--storage-state", settings.storageStatePath());
    int outIdx = cmd.indexOf("--out");
    assertThat(outIdx).isGreaterThan(0);
    assertThat(cmd.get(outIdx + 1)).endsWith(".m4a");
  }

  @Test
  void embedsDisclosureMarkerForMp3Payloads(@TempDir Path tmp) throws IOException {
    byte[] mockAudio = "ID3FAKEAUDIO".getBytes(StandardCharsets.ISO_8859_1);
    var captured = new CapturingRunner(mockAudio, 0, "{}\n", "");
    var provider = new NotebookLmAudioOverviewProvider(settingsWith(tmp, Map.of()), captured);

    Artifact artifact = provider.generateOverview(spec("hello world"));

    assertThat(artifact.contentType()).isEqualTo("audio/mpeg");
    assertThat(artifact.extension()).isEqualTo(".mp3");
    assertThat(artifact.bytes()).startsWith("ID3".getBytes(StandardCharsets.ISO_8859_1));
    assertThat(new String(artifact.bytes(), StandardCharsets.ISO_8859_1))
        .contains("AI-generated audio")
        .contains("ID3FAKEAUDIO");
  }

  @Test
  void nonZeroExitMapsToProviderException(@TempDir Path tmp) throws IOException {
    var settings = settingsWith(tmp, Map.of());
    var runner = new CapturingRunner(new byte[0], 2, "", "session expired");
    var provider = new NotebookLmAudioOverviewProvider(settings, runner);
    assertThatThrownBy(() -> provider.generateOverview(spec("anything")))
        .isInstanceOf(GenerationProviderException.class)
        .hasMessageContaining("session expired")
        .satisfies(e -> assertThat(((GenerationProviderException) e).getCode())
            .isEqualTo("NOTEBOOKLM_RPC_FAILED"));
  }

  @Test
  void emptyOutputFileMapsToProviderException(@TempDir Path tmp) throws IOException {
    var settings = settingsWith(tmp, Map.of());
    var runner = new CapturingRunner(new byte[0], 0, "{}\n", "");
    var provider = new NotebookLmAudioOverviewProvider(settings, runner);
    assertThatThrownBy(() -> provider.generateOverview(spec("anything")))
        .isInstanceOf(GenerationProviderException.class)
        .satisfies(e -> assertThat(((GenerationProviderException) e).getCode())
            .isEqualTo("NOTEBOOKLM_EMPTY_ARTIFACT"));
  }

  @Test
  void exitOneClassifiedAsNotConfiguredCode(@TempDir Path tmp) throws IOException {
    var settings = settingsWith(tmp, Map.of());
    var runner = new CapturingRunner(new byte[0], 1, "", "storage_state.json missing");
    var provider = new NotebookLmAudioOverviewProvider(settings, runner);
    assertThatThrownBy(() -> provider.generateOverview(spec("x")))
        .isInstanceOf(GenerationProviderException.class)
        .satisfies(e -> assertThat(((GenerationProviderException) e).getCode())
            .isEqualTo("NOTEBOOKLM_NOT_CONFIGURED"));
  }

  @Test
  void ioFailurePropagatesAsProviderException(@TempDir Path tmp) throws IOException {
    var settings = settingsWith(tmp, Map.of());
    ProcessRunner failing = (cmd, t) -> {
      throw new IOException("python3 not found");
    };
    var provider = new NotebookLmAudioOverviewProvider(settings, failing);
    assertThatThrownBy(() -> provider.generateOverview(spec("x")))
        .isInstanceOf(GenerationProviderException.class)
        .hasMessageContaining("python3 not found")
        .satisfies(e -> assertThat(((GenerationProviderException) e).getCode())
            .isEqualTo("NOTEBOOKLM_IO_FAILED"));
  }

  // helpers ----------------------------------------------------------------

  private static NotebookLmBridgeSettings settingsWith(Path tmp, Map<String, String> overrides) throws IOException {
    Path storage = Files.createFile(tmp.resolve("storage_state.json"));
    Map<String, String> env = new HashMap<>(overrides);
    env.putIfAbsent("NOTEBOOKLM_STORAGE_STATE_PATH", storage.toAbsolutePath().toString());
    return NotebookLmBridgeSettings.fromEnvironment(env);
  }

  private static JobSpec spec(String prompt) {
    return new JobSpec(
        "job-test",
        "media-test",
        "tenant-test",
        GenerationOutputType.AUDIO,
        prompt,
        "notebooklm",
        null,
        null,
        Map.of());
  }

  private static byte[] fakeM4a() {
    return new byte[] {
        0, 0, 0, 24,
        'f', 't', 'y', 'p',
        'd', 'a', 's', 'h',
        0, 0, 0, 0,
        'i', 's', 'o', '6',
        'm', 'p', '4', '1'
    };
  }

  /**
   * Stub runner that writes pre-canned bytes to whatever --out path the
   * provider passes, then returns the configured exit code + streams. Captures
   * the most recent command so assertions can inspect argv.
   */
  private static final class CapturingRunner implements ProcessRunner {
    private final byte[] payload;
    private final int exitCode;
    private final String stdout;
    private final String stderr;
    private List<String> lastCommand;

    private CapturingRunner(byte[] payload, int exitCode, String stdout, String stderr) {
      this.payload = payload;
      this.exitCode = exitCode;
      this.stdout = stdout;
      this.stderr = stderr;
    }

    @Override
    public Result run(List<String> command, Duration timeout) throws IOException {
      this.lastCommand = List.copyOf(command);
      int outIdx = command.indexOf("--out");
      if (outIdx >= 0 && outIdx + 1 < command.size() && payload.length > 0) {
        Files.write(Path.of(command.get(outIdx + 1)), payload);
      }
      return new Result(exitCode, stdout, stderr);
    }

    private List<String> lastCommand() {
      return lastCommand;
    }
  }
}
