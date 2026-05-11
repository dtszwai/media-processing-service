package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.GenerationErrorCode;

import com.mediaservice.providers.generation.audio.AudioOverviewProvider;
import com.mediaservice.providers.generation.image.ImageProvider;
import com.mediaservice.providers.generation.llm.LlmProvider;
import com.mediaservice.providers.generation.moderation.ModerationProvider;
import java.util.Locale;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.atomic.AtomicReference;
import java.util.function.Supplier;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import com.mediaservice.providers.generation.audio.NotebookLmAudioOverviewProvider;
import com.mediaservice.providers.generation.audio.SimulatedAudioOverviewProvider;
import com.mediaservice.providers.generation.image.OpenAIImageProvider;
import com.mediaservice.providers.generation.image.SimulatedGpuProvider;
import com.mediaservice.providers.generation.llm.OpenAILlmProvider;
import com.mediaservice.providers.generation.llm.SimulatedLlmProvider;
import com.mediaservice.providers.generation.moderation.SimulatedModerationProvider;
import com.mediaservice.providers.generation.prompt.NoopPromptEnhancer;
import com.mediaservice.providers.generation.prompt.PromptEnhancer;
import com.mediaservice.providers.generation.prompt.SimulatedPromptEnhancer;
import com.mediaservice.providers.generation.shared.NotebookLmBridgeSettings;
import com.mediaservice.providers.generation.shared.OpenAIClient;

public class GenerationProviderFactory {
  private final GenerationRuntimeConfig config;
  private final DynamoDbClient dynamoDbClient;
  private final String tableName;
  private final GenerationMetrics metrics;

  private final Object lock = new Object();
  private final AtomicReference<OpenAIClient> openAiClient = new AtomicReference<>();
  private final AtomicReference<ImageProvider> imageProvider = new AtomicReference<>();
  private final AtomicReference<ModerationProvider> moderationProvider = new AtomicReference<>();
  private final AtomicReference<AudioOverviewProvider> audioOverviewProvider = new AtomicReference<>();
  private final ConcurrentHashMap<String, AudioOverviewProvider> audioOverviewProvidersByName =
      new ConcurrentHashMap<>();
  private final AtomicReference<LlmProvider> llmProvider = new AtomicReference<>();
  private final AtomicReference<PromptEnhancer> promptEnhancer = new AtomicReference<>();

  public GenerationProviderFactory(GenerationRuntimeConfig config, DynamoDbClient dynamoDbClient, String tableName) {
    this(config, dynamoDbClient, tableName, GenerationMetrics.noop());
  }

  public GenerationProviderFactory(GenerationRuntimeConfig config, DynamoDbClient dynamoDbClient, String tableName,
      GenerationMetrics metrics) {
    this.config = config;
    this.dynamoDbClient = dynamoDbClient;
    this.tableName = tableName;
    this.metrics = metrics != null ? metrics : GenerationMetrics.noop();
  }

  public ImageProvider imageProvider() {
    return lazyInit(imageProvider, lock, this::buildImageProvider);
  }

  public ModerationProvider moderationProvider() {
    return lazyInit(moderationProvider, lock, this::buildModerationProvider);
  }

  public PromptEnhancer promptEnhancer() {
    return lazyInit(promptEnhancer, lock,
        () -> config.promptEnhancementEnabled() ? new SimulatedPromptEnhancer() : new NoopPromptEnhancer());
  }

  public AudioOverviewProvider audioOverviewProvider() {
    return lazyInit(audioOverviewProvider, lock,
        () -> buildAudioOverviewProvider(config.audioOverviewProvider().toLowerCase(Locale.ROOT)));
  }

  public AudioOverviewProvider audioOverviewProvider(String name) {
    if (name == null || name.isBlank()) {
      return audioOverviewProvider();
    }
    String normalized = name.toLowerCase(Locale.ROOT);
    if (normalized.equals(config.audioOverviewProvider().toLowerCase(Locale.ROOT))) {
      return audioOverviewProvider();
    }
    return audioOverviewProvidersByName.computeIfAbsent(normalized, this::buildAudioOverviewProvider);
  }

  private AudioOverviewProvider buildAudioOverviewProvider(String name) {
    if ("simulated".equals(name) || "simulator".equals(name)) {
      return new SimulatedAudioOverviewProvider();
    }
    if ("notebooklm".equals(name)) {
      return new NotebookLmAudioOverviewProvider(NotebookLmBridgeSettings.fromEnvironment(System.getenv()));
    }
    throw new GenerationProviderException(GenerationErrorCode.UNSUPPORTED_PROVIDER, "Unsupported audio overview provider: " + name);
  }

  public LlmProvider llmProvider() {
    return lazyInit(llmProvider, lock, this::buildLlmProvider);
  }

  public static GenerationProviderFactory fromEnvironment(DynamoDbClient dynamoDbClient, String tableName) {
    return new GenerationProviderFactory(GenerationRuntimeConfig.fromEnvironment(System.getenv()), dynamoDbClient, tableName);
  }

  public GenerationRuntimeConfig config() {
    return config;
  }

  private ImageProvider buildImageProvider() {
    String provider = config.provider().toLowerCase(Locale.ROOT);
    if ("simulated".equals(provider) || "simulator".equals(provider)) {
      return new SimulatedGpuProvider(config, dynamoDbClient, tableName);
    }
    if ("openai".equals(provider)) {
      return new OpenAIImageProvider(config, openAiClient());
    }
    throw new GenerationProviderException(GenerationErrorCode.UNSUPPORTED_PROVIDER, "Unsupported generation provider: " + provider);
  }

  private ModerationProvider buildModerationProvider() {
    String provider = config.moderationProvider().toLowerCase(Locale.ROOT);
    if ("simulated".equals(provider) || "simulator".equals(provider)) {
      return new SimulatedModerationProvider();
    }
    throw new GenerationProviderException(GenerationErrorCode.UNSUPPORTED_PROVIDER, "Unsupported moderation provider: " + provider);
  }

  private LlmProvider buildLlmProvider() {
    String provider = config.llmProvider().toLowerCase(Locale.ROOT);
    if ("simulated".equals(provider) || "simulator".equals(provider)) {
      return new SimulatedLlmProvider();
    }
    if ("openai".equals(provider)) {
      return new OpenAILlmProvider(config, openAiClient());
    }
    throw new GenerationProviderException(GenerationErrorCode.UNSUPPORTED_PROVIDER, "Unsupported LLM provider: " + provider);
  }

  private OpenAIClient openAiClient() {
    return lazyInit(openAiClient, lock, () -> new OpenAIClient(config, defaultHttpClient(), metrics));
  }

  private <T> T lazyInit(AtomicReference<T> ref, Object lock, Supplier<T> factory) {
    T local = ref.get();
    if (local == null) {
      synchronized (lock) {
        local = ref.get();
        if (local == null) {
          local = factory.get();
          ref.set(local);
        }
      }
    }
    return local;
  }

  private static java.net.http.HttpClient defaultHttpClient() {
    return java.net.http.HttpClient.newBuilder()
        .connectTimeout(java.time.Duration.ofSeconds(5))
        .build();
  }
}
