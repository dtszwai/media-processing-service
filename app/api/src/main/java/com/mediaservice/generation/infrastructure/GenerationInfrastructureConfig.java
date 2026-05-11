package com.mediaservice.generation.infrastructure;

import com.mediaservice.providers.generation.core.DynamoDbGenerationRepository;
import com.mediaservice.providers.generation.core.GeneratedAssetStorage;
import com.mediaservice.providers.generation.core.GenerationAdmissionController;
import com.mediaservice.providers.generation.core.GenerationMetrics;
import com.mediaservice.providers.generation.core.GenerationProviderFactory;
import com.mediaservice.providers.generation.core.GenerationRepository;
import com.mediaservice.providers.generation.core.GenerationRuntimeConfig;
import com.mediaservice.providers.generation.core.GenerationWorkflow;
import com.mediaservice.providers.generation.core.OpenTelemetryGenerationMetrics;
import com.mediaservice.providers.generation.core.WebhookNotifier;
import com.mediaservice.shared.cache.CacheInvalidationService;
import io.opentelemetry.api.metrics.Meter;
import java.util.HashMap;
import java.util.Map;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.core.env.Environment;
import org.springframework.data.redis.core.StringRedisTemplate;
import software.amazon.awssdk.services.dynamodb.DynamoDbClient;
import software.amazon.awssdk.services.sqs.SqsClient;

@Configuration
public class GenerationInfrastructureConfig {

  /**
   * Mapping of GENERATION_* environment-variable names to the Spring property keys
   * (and explicit {@code GENERATION_*} aliases). This lets profile-specific YAML
   * overrides drive {@link GenerationRuntimeConfig} while preserving the existing
   * env-driven contract documented in the build plan.
   */
  private static final Map<String, String[]> GENERATION_ENV_BINDINGS = buildBindings();

  @Bean
  public GenerationRuntimeConfig generationRuntimeConfig(Environment environment) {
    Map<String, String> env = new HashMap<>(System.getenv());
    GENERATION_ENV_BINDINGS.forEach((envKey, propertyKeys) -> {
      for (String propertyKey : propertyKeys) {
        String value = environment.getProperty(propertyKey);
        if (value != null && !value.isBlank()) {
          env.put(envKey, value);
          return;
        }
      }
    });
    return GenerationRuntimeConfig.fromEnvironment(env);
  }

  @Bean
  public GenerationProviderFactory generationProviderFactory(GenerationRuntimeConfig config, DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    return new GenerationProviderFactory(config, dynamoDbClient, tableName);
  }

  @Bean
  public GenerationRepository generationRepository(DynamoDbClient dynamoDbClient,
      @Value("${aws.dynamodb.table-name}") String tableName) {
    return new DynamoDbGenerationRepository(dynamoDbClient, tableName);
  }

  @Bean
  public GenerationAdmissionController generationAdmissionController(SqsClient sqsClient,
      StringRedisTemplate redisTemplate,
      @Value("${media.generation.queue-url:}") String queueUrl,
      @Value("${media.generation.backpressure.max-queue-depth:500}") int maxQueueDepth,
      @Value("${media.generation.backpressure.delayed-threshold-pct:60}") int delayedThresholdPct,
      @Value("${media.generation.backpressure.degraded-threshold-pct:80}") int degradedThresholdPct,
      @Value("${media.generation.backpressure.retry-after-seconds:30}") int retryAfterSeconds,
      @Value("${media.generation.backpressure.fail-open:true}") boolean failOpen,
      @Value("${media.generation.admission.free-daily-limit:25}") int freeDailyLimit,
      @Value("${media.generation.admission.paid-daily-limit:1000}") int paidDailyLimit,
      @Value("${media.generation.admission.free-monthly-limit:250}") int freeMonthlyLimit,
      @Value("${media.generation.admission.paid-monthly-limit:30000}") int paidMonthlyLimit,
      @Value("${media.generation.admission.free-outstanding-limit:2}") int freeOutstandingLimit,
      @Value("${media.generation.admission.paid-outstanding-limit:50}") int paidOutstandingLimit) {
    GenerationAdmissionController resourceController = new SqsGenerationAdmissionController(sqsClient, queueUrl, maxQueueDepth, delayedThresholdPct,
        degradedThresholdPct, retryAfterSeconds, failOpen);
    return new RedisGenerationAdmissionController(redisTemplate, resourceController, freeDailyLimit, paidDailyLimit,
        freeMonthlyLimit, paidMonthlyLimit, freeOutstandingLimit, paidOutstandingLimit);
  }

  @Bean
  public GenerationMetrics generationMetrics(Meter meter, GenerationRuntimeConfig config,
      @Value("${spring.application.name:media-service-api}") String serviceName) {
    return new OpenTelemetryGenerationMetrics(meter, serviceName, config.region());
  }

  @Bean
  public WebhookNotifier apiWebhookNotifier(CacheInvalidationService cacheInvalidationService) {
    return media -> cacheInvalidationService.invalidateMedia(media.getMediaId());
  }

  @Bean
  public GenerationWorkflow generationWorkflow(GenerationRepository repository, GenerationProviderFactory providerFactory,
      GeneratedAssetStorage storage, GenerationJobPublisher publisher, WebhookNotifier webhookNotifier,
      GenerationAdmissionController admissionController, GenerationMetrics metrics) {
    return new GenerationWorkflow(repository, providerFactory, storage, publisher, webhookNotifier, admissionController, metrics);
  }

  private static Map<String, String[]> buildBindings() {
    Map<String, String[]> b = new HashMap<>();
    b.put("GENERATION_PROVIDER", new String[]{"media.generation.provider", "GENERATION_PROVIDER"});
    b.put("GENERATION_MODERATION_PROVIDER", new String[]{"media.generation.moderation-provider", "GENERATION_MODERATION_PROVIDER"});
    b.put("GENERATION_AUDIO_OVERVIEW_PROVIDER", new String[]{"media.generation.audio-overview-provider", "GENERATION_AUDIO_OVERVIEW_PROVIDER"});
    b.put("GENERATION_REGION", new String[]{"media.generation.region", "aws.region", "GENERATION_REGION"});
    b.put("GENERATION_MODEL", new String[]{"media.generation.model", "GENERATION_MODEL"});
    b.put("GENERATION_SIMULATOR_KIND", new String[]{"media.generation.simulator.kind", "GENERATION_SIMULATOR_KIND"});
    b.put("GENERATION_SIMULATOR_MEAN_DURATION_MS", new String[]{"media.generation.simulator.mean-duration-ms", "GENERATION_SIMULATOR_MEAN_DURATION_MS"});
    b.put("GENERATION_SIMULATOR_COLD_START_MS", new String[]{"media.generation.simulator.cold-start-ms", "GENERATION_SIMULATOR_COLD_START_MS"});
    b.put("GENERATION_SIMULATOR_FAILURE_RATE", new String[]{"media.generation.simulator.failure-rate", "GENERATION_SIMULATOR_FAILURE_RATE"});
    b.put("GENERATION_SIMULATOR_CHAOS_BUSINESS_HOURS_ENABLED", new String[]{"media.generation.chaos.enabled", "GENERATION_SIMULATOR_CHAOS_BUSINESS_HOURS_ENABLED"});
    b.put("GENERATION_SIMULATOR_CHAOS_FAILURE_RATE", new String[]{"media.generation.chaos.failure-rate", "GENERATION_SIMULATOR_CHAOS_FAILURE_RATE"});
    b.put("GENERATION_SIMULATOR_CHAOS_START_HOUR_UTC", new String[]{"media.generation.chaos.start-hour-utc", "GENERATION_SIMULATOR_CHAOS_START_HOUR_UTC"});
    b.put("GENERATION_SIMULATOR_CHAOS_END_HOUR_UTC", new String[]{"media.generation.chaos.end-hour-utc", "GENERATION_SIMULATOR_CHAOS_END_HOUR_UTC"});
    b.put("GENERATION_BUDGET_DAILY_USD", new String[]{"media.generation.budget-daily-usd", "GENERATION_BUDGET_DAILY_USD"});
    b.put("GENERATION_BUDGET_ALERT_PCT", new String[]{"media.generation.budget-alert-pct", "GENERATION_BUDGET_ALERT_PCT"});
    b.put("GENERATION_PROVIDER_TIMEOUT_MS", new String[]{"media.generation.provider-timeout-ms", "GENERATION_PROVIDER_TIMEOUT_MS"});
    b.put("GENERATION_OPENAI_API_KEY", new String[]{"media.generation.openai.api-key", "GENERATION_OPENAI_API_KEY"});
    b.put("GENERATION_OPENAI_API_KEY_SECRET_ARN", new String[]{"media.generation.openai.api-key-secret-arn", "GENERATION_OPENAI_API_KEY_SECRET_ARN"});
    b.put("GENERATION_PROMPT_ENHANCEMENT_ENABLED", new String[]{"media.generation.prompt-enhancement-enabled", "GENERATION_PROMPT_ENHANCEMENT_ENABLED"});
    b.put("GENERATION_STAGE_MAX_ATTEMPTS", new String[]{"media.generation.stage-max-attempts", "GENERATION_STAGE_MAX_ATTEMPTS"});
    return b;
  }
}
