package com.mediaservice.providers.generation.shared;

import com.mediaservice.common.generation.GenerationErrorCode;

import com.fasterxml.jackson.databind.JsonNode;
import com.fasterxml.jackson.databind.ObjectMapper;
import java.io.IOException;
import java.net.URI;
import java.net.http.HttpClient;
import java.net.http.HttpRequest;
import java.net.http.HttpResponse;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Optional;
import java.util.concurrent.ThreadLocalRandom;
import java.util.function.Supplier;
import software.amazon.awssdk.auth.credentials.DefaultCredentialsProvider;
import software.amazon.awssdk.regions.Region;
import software.amazon.awssdk.services.secretsmanager.SecretsManagerClient;
import software.amazon.awssdk.services.secretsmanager.model.GetSecretValueRequest;
import com.mediaservice.providers.generation.core.GenerationMetrics;
import com.mediaservice.providers.generation.core.GenerationProviderException;
import com.mediaservice.providers.generation.core.GenerationRuntimeConfig;
import com.mediaservice.providers.generation.core.NotConfiguredException;

public final class OpenAIClient {
  private static final URI API_BASE = URI.create("https://api.openai.com/v1");
  private static final ObjectMapper MAPPER = new ObjectMapper();
  private static final Duration CONNECT_TIMEOUT = Duration.ofSeconds(5);
  private static final long BASE_BACKOFF_MILLIS = 250L;
  private static final long MAX_BACKOFF_MILLIS = 5_000L;

  private final GenerationRuntimeConfig config;
  private final HttpClient httpClient;
  private final GenerationMetrics metrics;

  // Rotation-aware cache: value + epoch-millis it was fetched. Volatile for visibility.
  private volatile String cachedApiKey;
  private volatile long cachedApiKeyFetchedAt;

  // Lazily-built, reused across secret reads. Package-private so tests can substitute a fake.
  volatile SecretsManagerClient secretsManagerClient;

  public OpenAIClient(GenerationRuntimeConfig config) {
    this(config, HttpClient.newBuilder()
        .connectTimeout(CONNECT_TIMEOUT)
        .build(), GenerationMetrics.noop());
  }

  public OpenAIClient(GenerationRuntimeConfig config, HttpClient httpClient) {
    this(config, httpClient, GenerationMetrics.noop());
  }

  public OpenAIClient(GenerationRuntimeConfig config, HttpClient httpClient, GenerationMetrics metrics) {
    this.config = config;
    this.httpClient = httpClient;
    this.metrics = metrics != null ? metrics : GenerationMetrics.noop();
  }

  public void requireConfigured() {
    apiKey();
  }

  public static ObjectMapper mapper() {
    return MAPPER;
  }

  public JsonNode postJson(String path, JsonNode body, String clientRequestId) {
    return execute(path, body, clientRequestId, HttpResponse.BodyHandlers.ofString(),
        text -> text != null ? text : "",
        text -> {
          try {
            return MAPPER.readTree(text);
          } catch (Exception e) {
            throw new GenerationProviderException(GenerationErrorCode.OPENAI_REQUEST_FAILED, e.getMessage());
          }
        },
        true);
  }

  public byte[] postBytes(String path, JsonNode body, String clientRequestId) {
    return execute(path, body, clientRequestId, HttpResponse.BodyHandlers.ofByteArray(),
        bytes -> bytes != null ? new String(bytes, StandardCharsets.UTF_8) : "",
        bytes -> bytes,
        false);
  }

  /**
   * Shared POST execution with retry, jitter, secret refresh, OTel timing, and uniform error
   * classification. {@code postJson} and {@code postBytes} differ only in their HTTP body type
   * and response decoding; everything else (auth, idempotency header, retry policy) is identical.
   *
   * @param accept whether to set {@code Accept: application/json}
   */
  private <T, R> R execute(String path, JsonNode body, String clientRequestId,
      HttpResponse.BodyHandler<T> handler,
      java.util.function.Function<T, String> bodyToString,
      java.util.function.Function<T, R> decode,
      boolean accept) {
    return retry(() -> {
      try {
        HttpRequest.Builder builder = baseRequest(API_BASE.resolve(path), clientRequestId)
            .header("Content-Type", "application/json");
        if (accept) {
          builder.header("Accept", "application/json");
        }
        HttpRequest request = builder
            .POST(HttpRequest.BodyPublishers.ofString(MAPPER.writeValueAsString(body)))
            .build();
        HttpResponse<T> response = httpClient.send(request, handler);
        requireSuccess(response.statusCode(), bodyToString.apply(response.body()));
        return decode.apply(response.body());
      } catch (GenerationProviderException e) {
        throw e;
      } catch (IOException | InterruptedException e) {
        if (e instanceof InterruptedException) {
          Thread.currentThread().interrupt();
        }
        throw new GenerationProviderException(GenerationErrorCode.OPENAI_REQUEST_FAILED, e.getMessage());
      } catch (Exception e) {
        throw new GenerationProviderException(GenerationErrorCode.OPENAI_REQUEST_FAILED, e.getMessage());
      }
    });
  }

  public byte[] getBytes(String url) {
    return retry(() -> {
      try {
        HttpRequest request = HttpRequest.newBuilder(URI.create(url))
            .timeout(config.providerTimeout())
            .GET()
            .build();
        HttpResponse<byte[]> response = httpClient.send(request, HttpResponse.BodyHandlers.ofByteArray());
        byte[] bytes = response.body();
        String bodyText = bytes != null ? new String(bytes, StandardCharsets.UTF_8) : "";
        requireSuccess(response.statusCode(), bodyText);
        return bytes;
      } catch (GenerationProviderException e) {
        throw e;
      } catch (IOException | InterruptedException e) {
        if (e instanceof InterruptedException) {
          Thread.currentThread().interrupt();
        }
        throw new GenerationProviderException(GenerationErrorCode.OPENAI_ARTIFACT_FETCH_FAILED, e.getMessage());
      } catch (Exception e) {
        throw new GenerationProviderException(GenerationErrorCode.OPENAI_ARTIFACT_FETCH_FAILED, e.getMessage());
      }
    });
  }

  /**
   * Execute {@code call} with bounded retry, exponential backoff and full jitter.
   * Retryable: IOException-wrapped {@code OPENAI_REQUEST_FAILED}, 5xx, 429, and one-shot 401 refresh.
   */
  private <T> T retry(Supplier<T> call) {
    int maxAttempts = Math.max(1, config.maxProviderAttempts());
    boolean refreshedSecret = false;
    GenerationProviderException last = null;
    for (int attempt = 1; attempt <= maxAttempts; attempt++) {
      try {
        return call.get();
      } catch (GenerationProviderException e) {
        last = e;
        // On 401: refresh secret once and retry without consuming a normal attempt.
        if ("OPENAI_UNAUTHORIZED".equals(e.getCode()) && !refreshedSecret) {
          refreshedSecret = true;
          forceRefreshApiKey();
          metrics.recordProviderRetry("openai", attempt, "secret_refresh");
          continue;
        }
        if (!isRetryable(e.getCode()) || attempt >= maxAttempts) {
          throw e;
        }
        metrics.recordProviderRetry("openai", attempt, e.getCode());
        sleepWithJitter(attempt);
      }
    }
    throw last != null ? last : new GenerationProviderException(GenerationErrorCode.OPENAI_REQUEST_FAILED, "retry exhausted");
  }

  private boolean isRetryable(String code) {
    return switch (code) {
      case "OPENAI_RATE_LIMITED",
          "OPENAI_SERVER_ERROR",
          "OPENAI_REQUEST_FAILED",
          "OPENAI_ARTIFACT_FETCH_FAILED" -> true;
      default -> false;
    };
  }

  private void sleepWithJitter(int attempt) {
    long expBackoff = Math.min(MAX_BACKOFF_MILLIS, BASE_BACKOFF_MILLIS * (1L << Math.min(10, attempt - 1)));
    long sleepMs = ThreadLocalRandom.current().nextLong(0, expBackoff + 1L);
    try {
      Thread.sleep(sleepMs);
    } catch (InterruptedException ie) {
      Thread.currentThread().interrupt();
    }
  }

  private HttpRequest.Builder baseRequest(URI uri, String clientRequestId) {
    HttpRequest.Builder builder = HttpRequest.newBuilder(uri)
        .timeout(config.providerTimeout())
        .header("Authorization", "Bearer " + apiKey());
    if (clientRequestId != null && !clientRequestId.isBlank()) {
      // Idempotency-Key is the defense-in-depth header expected by vendors. Keep the
      // legacy X-Client-Request-Id header for multi-vendor correlation.
      builder.header("Idempotency-Key", clientRequestId);
      builder.header("X-Client-Request-Id", clientRequestId);
    }
    return builder;
  }

  private void requireSuccess(int statusCode, String body) {
    if (statusCode >= 200 && statusCode < 300) {
      return;
    }
    String message = extractErrorMessage(body).orElse("OpenAI request failed with status " + statusCode);
    GenerationErrorCode code;
    if (statusCode == 401) {
      code = GenerationErrorCode.OPENAI_UNAUTHORIZED;
    } else if (statusCode == 403) {
      code = GenerationErrorCode.OPENAI_FORBIDDEN;
    } else if (statusCode == 429) {
      code = GenerationErrorCode.OPENAI_RATE_LIMITED;
    } else if (statusCode >= 500 && statusCode < 600) {
      code = GenerationErrorCode.OPENAI_SERVER_ERROR;
    } else if (statusCode >= 400 && statusCode < 500) {
      code = GenerationErrorCode.OPENAI_CLIENT_ERROR;
    } else {
      code = GenerationErrorCode.OPENAI_REQUEST_FAILED;
    }
    throw new GenerationProviderException(code, message);
  }

  private Optional<String> extractErrorMessage(String body) {
    if (body == null || body.isBlank()) {
      return Optional.empty();
    }
    try {
      JsonNode message = MAPPER.readTree(body).path("error").path("message");
      return message.isTextual() ? Optional.of(message.asText()) : Optional.empty();
    } catch (Exception ignored) {
      return Optional.empty();
    }
  }

  private String apiKey() {
    // Env-pinned API key wins (e.g. local dev). No caching needed; env is static per process.
    if (config.openAiApiKey() != null && !config.openAiApiKey().isBlank()) {
      return config.openAiApiKey();
    }
    long ttl = Math.max(0L, config.secretCacheTtlMillis());
    long now = System.currentTimeMillis();
    String snapshot = cachedApiKey;
    if (snapshot != null && (ttl == 0 || now - cachedApiKeyFetchedAt < ttl)) {
      return snapshot;
    }
    return refreshApiKeyFromSecret(false);
  }

  private void forceRefreshApiKey() {
    // Env-pinned: there is no Secrets Manager source to re-fetch from. Surface the refresh
    // metric so callers see the attempt, then return — the next retry uses the same env key.
    if (config.openAiApiKey() != null && !config.openAiApiKey().isBlank()) {
      metrics.recordSecretRefresh("openai");
      return;
    }
    refreshApiKeyFromSecret(true);
  }

  private synchronized String refreshApiKeyFromSecret(boolean forced) {
    // Re-check under the lock so a racing caller does not re-fetch.
    long ttl = Math.max(0L, config.secretCacheTtlMillis());
    long now = System.currentTimeMillis();
    if (!forced && cachedApiKey != null && (ttl == 0 || now - cachedApiKeyFetchedAt < ttl)) {
      return cachedApiKey;
    }
    if (config.openAiApiKeySecretArn() == null || config.openAiApiKeySecretArn().isBlank()) {
      throw new NotConfiguredException("OpenAIProvider",
          "GENERATION_OPENAI_API_KEY or GENERATION_OPENAI_API_KEY_SECRET_ARN");
    }
    String fresh = readSecretApiKey();
    cachedApiKey = fresh;
    cachedApiKeyFetchedAt = System.currentTimeMillis();
    if (forced) {
      metrics.recordSecretRefresh("openai");
    }
    return fresh;
  }

  private String readSecretApiKey() {
    try {
      // Reuse the cached SecretsManagerClient across refreshes. The previous implementation
      // built and closed a fresh client on every secret read; that ran the SDK's region/credential
      // provider chain on every TTL refresh and 401 retry.
      SecretsManagerClient secrets = secretsClient();
      String secret = secrets.getSecretValue(GetSecretValueRequest.builder()
          .secretId(config.openAiApiKeySecretArn())
          .build()).secretString();
      String apiKey = parseSecretString(secret);
      if (apiKey == null || apiKey.isBlank() || "not-configured".equals(apiKey)) {
        throw new NotConfiguredException("OpenAIProvider", "valid OpenAI API key secret value");
      }
      return apiKey;
    } catch (GenerationProviderException e) {
      throw e;
    } catch (Exception e) {
      throw new GenerationProviderException(GenerationErrorCode.OPENAI_SECRET_READ_FAILED, e.getMessage());
    }
  }

  private SecretsManagerClient secretsClient() {
    SecretsManagerClient local = secretsManagerClient;
    if (local == null) {
      synchronized (this) {
        local = secretsManagerClient;
        if (local == null) {
          var builder = SecretsManagerClient.builder()
              .region(Region.of(config.region()))
              .credentialsProvider(DefaultCredentialsProvider.create());
          String endpoint = Optional.ofNullable(System.getenv("AWS_SECRETSMANAGER_ENDPOINT"))
              .orElse(System.getenv("AWS_ENDPOINT_URL_SECRETSMANAGER"));
          if (endpoint != null && !endpoint.isBlank()) {
            builder.endpointOverride(URI.create(endpoint));
          }
          local = builder.build();
          secretsManagerClient = local;
        }
      }
    }
    return local;
  }

  private String parseSecretString(String secret) {
    if (secret == null || secret.isBlank()) {
      return null;
    }
    try {
      JsonNode json = MAPPER.readTree(secret);
      JsonNode apiKey = json.path("api_key");
      if (apiKey.isTextual()) {
        return apiKey.asText();
      }
    } catch (Exception ignored) {
      return secret;
    }
    return secret;
  }
}
