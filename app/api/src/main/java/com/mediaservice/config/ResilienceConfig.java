package com.mediaservice.config;

import io.github.resilience4j.circuitbreaker.CircuitBreaker;
import io.github.resilience4j.circuitbreaker.CircuitBreakerConfig;
import io.github.resilience4j.circuitbreaker.CircuitBreakerRegistry;
import io.github.resilience4j.core.IntervalFunction;
import io.github.resilience4j.retry.Retry;
import io.github.resilience4j.retry.RetryConfig;
import io.github.resilience4j.retry.RetryRegistry;
import io.github.resilience4j.timelimiter.TimeLimiter;
import io.github.resilience4j.timelimiter.TimeLimiterConfig;
import io.github.resilience4j.timelimiter.TimeLimiterRegistry;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import software.amazon.awssdk.core.exception.SdkClientException;
import software.amazon.awssdk.services.dynamodb.model.DynamoDbException;
import software.amazon.awssdk.services.s3.model.S3Exception;
import software.amazon.awssdk.services.sns.model.SnsException;

import java.io.IOException;
import java.time.Duration;
import java.util.concurrent.TimeoutException;

@Configuration
public class ResilienceConfig {
  public static final String S3_CIRCUIT_BREAKER = "s3CircuitBreaker";
  public static final String DYNAMODB_CIRCUIT_BREAKER = "dynamoDbCircuitBreaker";
  public static final String SNS_CIRCUIT_BREAKER = "snsCircuitBreaker";

  @Bean
  public CircuitBreakerRegistry circuitBreakerRegistry() {
    CircuitBreakerConfig defaultConfig = CircuitBreakerConfig.custom()
        .failureRateThreshold(50)
        .slowCallRateThreshold(80)
        .slowCallDurationThreshold(Duration.ofSeconds(5))
        .waitDurationInOpenState(Duration.ofSeconds(30))
        .permittedNumberOfCallsInHalfOpenState(5)
        .slidingWindowType(CircuitBreakerConfig.SlidingWindowType.COUNT_BASED)
        .slidingWindowSize(10)
        .minimumNumberOfCalls(5)
        .recordExceptions(
            S3Exception.class,
            DynamoDbException.class,
            SnsException.class,
            SdkClientException.class,
            IOException.class,
            TimeoutException.class)
        .ignoreExceptions(
            IllegalArgumentException.class)
        .build();

    return CircuitBreakerRegistry.of(defaultConfig);
  }

  @Bean
  public CircuitBreaker s3CircuitBreaker(CircuitBreakerRegistry registry) {
    return registry.circuitBreaker(S3_CIRCUIT_BREAKER);
  }

  @Bean
  public CircuitBreaker dynamoDbCircuitBreaker(CircuitBreakerRegistry registry) {
    return registry.circuitBreaker(DYNAMODB_CIRCUIT_BREAKER);
  }

  @Bean
  public CircuitBreaker snsCircuitBreaker(CircuitBreakerRegistry registry) {
    return registry.circuitBreaker(SNS_CIRCUIT_BREAKER);
  }

  @Bean
  public RetryRegistry retryRegistry() {
    RetryConfig defaultConfig = RetryConfig.custom()
        .maxAttempts(3)
        .intervalFunction(IntervalFunction.ofExponentialBackoff(Duration.ofMillis(500), 2))
        .retryExceptions(
            S3Exception.class,
            DynamoDbException.class,
            SnsException.class,
            SdkClientException.class,
            IOException.class)
        .ignoreExceptions(
            IllegalArgumentException.class)
        .build();

    return RetryRegistry.of(defaultConfig);
  }

  @Bean
  public Retry s3Retry(RetryRegistry registry) {
    return registry.retry("s3Retry");
  }

  @Bean
  public Retry dynamoDbRetry(RetryRegistry registry) {
    return registry.retry("dynamoDbRetry");
  }

  @Bean
  public Retry snsRetry(RetryRegistry registry) {
    return registry.retry("snsRetry");
  }

  @Bean
  public TimeLimiterRegistry timeLimiterRegistry() {
    TimeLimiterConfig defaultConfig = TimeLimiterConfig.custom()
        .timeoutDuration(Duration.ofSeconds(10))
        .cancelRunningFuture(true)
        .build();

    return TimeLimiterRegistry.of(defaultConfig);
  }

  @Bean
  public TimeLimiter s3TimeLimiter(TimeLimiterRegistry registry) {
    return registry.timeLimiter("s3TimeLimiter");
  }
}
