package com.mediaservice.providers.generation.shared;

import java.io.IOException;
import java.io.InputStream;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.List;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.TimeUnit;
import com.mediaservice.providers.generation.audio.NotebookLmAudioOverviewProvider;

/**
 * Thin abstraction over {@link ProcessBuilder} so {@link NotebookLmAudioOverviewProvider}
 * stays unit-testable without spawning a real process.
 *
 * <p>The default implementation drains stdout and stderr concurrently using
 * Java 21 virtual threads — a subprocess that writes more than the OS pipe
 * buffer (~64KB on Linux) before exiting would otherwise deadlock against a
 * parent that only reads after {@link Process#waitFor}.
 */
@FunctionalInterface
public interface ProcessRunner {
  Result run(List<String> command, Duration timeout) throws IOException;

  record Result(int exitCode, String stdout, String stderr) {
  }

  static ProcessRunner systemDefault() {
    return (command, timeout) -> {
      Process process = new ProcessBuilder(command).redirectErrorStream(false).start();
      process.getOutputStream().close();
      CompletableFuture<String> stdout = readAsync(process.getInputStream());
      CompletableFuture<String> stderr = readAsync(process.getErrorStream());
      boolean finished;
      try {
        finished = process.waitFor(timeout.toMillis(), TimeUnit.MILLISECONDS);
      } catch (InterruptedException ie) {
        process.destroyForcibly();
        stdout.cancel(true);
        stderr.cancel(true);
        Thread.currentThread().interrupt();
        throw new IOException("interrupted while waiting for NotebookLM bridge", ie);
      }
      if (!finished) {
        process.destroyForcibly();
        stdout.cancel(true);
        stderr.cancel(true);
        throw new IOException("NotebookLM bridge timed out after " + timeout.toMillis() + "ms");
      }
      String stdoutText = awaitResult(stdout);
      String stderrText = awaitResult(stderr);
      return new Result(process.exitValue(), stdoutText, stderrText);
    };
  }

  private static CompletableFuture<String> readAsync(InputStream stream) {
    CompletableFuture<String> future = new CompletableFuture<>();
    Thread.ofVirtual().name("notebooklm-bridge-stream").start(() -> {
      try (stream) {
        future.complete(new String(stream.readAllBytes(), StandardCharsets.UTF_8));
      } catch (IOException e) {
        future.completeExceptionally(e);
      }
    });
    return future;
  }

  private static String awaitResult(CompletableFuture<String> future) throws IOException {
    try {
      return future.get();
    } catch (InterruptedException ie) {
      Thread.currentThread().interrupt();
      throw new IOException("interrupted draining NotebookLM bridge stream", ie);
    } catch (ExecutionException ee) {
      throw new IOException("failed draining NotebookLM bridge stream", ee.getCause());
    }
  }
}
