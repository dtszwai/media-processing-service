package com.mediaservice.config;

import com.fasterxml.jackson.annotation.JsonTypeInfo;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.fasterxml.jackson.databind.SerializationFeature;
import com.fasterxml.jackson.databind.jsontype.BasicPolymorphicTypeValidator;
import com.fasterxml.jackson.datatype.jsr310.JavaTimeModule;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.cache.CacheManager;
import org.springframework.cache.annotation.EnableCaching;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.data.redis.cache.RedisCacheConfiguration;
import org.springframework.data.redis.cache.RedisCacheManager;
import org.springframework.data.redis.connection.RedisConnectionFactory;
import org.springframework.data.redis.core.RedisTemplate;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.serializer.GenericJackson2JsonRedisSerializer;
import org.springframework.data.redis.serializer.RedisSerializationContext;
import org.springframework.data.redis.serializer.StringRedisSerializer;

import java.time.Duration;
import java.util.HashMap;
import java.util.Map;

/**
 * Redis configuration for caching and data operations.
 *
 * <p>
 * Configures Spring Cache with Redis backend, providing:
 * <ul>
 * <li>JSON serialization for complex objects</li>
 * <li>Per-cache TTL configuration</li>
 * <li>Connection pooling via Lettuce</li>
 * </ul>
 */
@Configuration
@EnableCaching
@RequiredArgsConstructor
@Slf4j
public class RedisConfig {

  public static final String CACHE_MEDIA = "media";
  public static final String CACHE_MEDIA_STATUS = "mediaStatus";
  public static final String CACHE_PRESIGNED_URL = "presignedUrl";
  public static final String CACHE_PAGINATION = "pagination";

  private final CacheProperties cacheProperties;

  /**
   * Create ObjectMapper for Redis JSON serialization.
   * Includes type information for proper deserialization of polymorphic types.
   * Note: This is not a @Bean to avoid overriding the default Spring MVC
   * ObjectMapper.
   */
  private ObjectMapper createRedisObjectMapper() {
    var mapper = new ObjectMapper();
    mapper.registerModule(new JavaTimeModule());
    mapper.disable(SerializationFeature.WRITE_DATES_AS_TIMESTAMPS);
    // Enable type information for polymorphic deserialization
    var ptv = BasicPolymorphicTypeValidator.builder()
        .allowIfBaseType(Object.class)
        .build();
    mapper.activateDefaultTyping(ptv, ObjectMapper.DefaultTyping.NON_FINAL, JsonTypeInfo.As.PROPERTY);
    return mapper;
  }

  /**
   * Configure RedisTemplate for general object operations.
   * Uses String keys and JSON-serialized values.
   */
  @Bean
  public RedisTemplate<String, Object> redisTemplate(RedisConnectionFactory connectionFactory) {
    var template = new RedisTemplate<String, Object>();
    template.setConnectionFactory(connectionFactory);
    var stringSerializer = new StringRedisSerializer();
    var jsonSerializer = new GenericJackson2JsonRedisSerializer(createRedisObjectMapper());
    template.setKeySerializer(stringSerializer);
    template.setValueSerializer(jsonSerializer);
    template.setHashKeySerializer(stringSerializer);
    template.setHashValueSerializer(jsonSerializer);
    template.afterPropertiesSet();
    log.info("RedisTemplate configured with JSON serialization");
    return template;
  }

  /**
   * Configure StringRedisTemplate for simple string operations.
   * Used for counters, rate limiting, and analytics.
   */
  @Bean
  public StringRedisTemplate stringRedisTemplate(RedisConnectionFactory connectionFactory) {
    return new StringRedisTemplate(connectionFactory);
  }

  /**
   * Configure CacheManager with per-cache TTL settings.
   */
  @Bean
  public CacheManager cacheManager(RedisConnectionFactory connectionFactory) {
    var jsonSerializer = new GenericJackson2JsonRedisSerializer(createRedisObjectMapper());
    var defaultConfig = RedisCacheConfiguration.defaultCacheConfig()
        .serializeKeysWith(RedisSerializationContext.SerializationPair.fromSerializer(new StringRedisSerializer()))
        .serializeValuesWith(RedisSerializationContext.SerializationPair.fromSerializer(jsonSerializer))
        .disableCachingNullValues()
        .entryTtl(Duration.ofMinutes(5));
    // Configure per-cache TTLs
    var cacheConfigurations = new HashMap<String, RedisCacheConfiguration>();
    cacheConfigurations.put(CACHE_MEDIA, defaultConfig
        .entryTtl(Duration.ofSeconds(cacheProperties.getMedia().getTtlSeconds())));
    cacheConfigurations.put(CACHE_MEDIA_STATUS, defaultConfig
        .entryTtl(Duration.ofSeconds(cacheProperties.getStatus().getTtlSeconds())));
    cacheConfigurations.put(CACHE_PRESIGNED_URL, defaultConfig
        .entryTtl(Duration.ofSeconds(cacheProperties.getPresignedUrl().getTtlSeconds())));
    cacheConfigurations.put(CACHE_PAGINATION, defaultConfig
        .entryTtl(Duration.ofSeconds(cacheProperties.getPagination().getTtlSeconds())));
    log.info("CacheManager configured with TTLs - media: {}s, status: {}s, presignedUrl: {}s, pagination: {}s",
        cacheProperties.getMedia().getTtlSeconds(),
        cacheProperties.getStatus().getTtlSeconds(),
        cacheProperties.getPresignedUrl().getTtlSeconds(),
        cacheProperties.getPagination().getTtlSeconds());
    return RedisCacheManager.builder(connectionFactory)
        .cacheDefaults(defaultConfig)
        .withInitialCacheConfigurations(cacheConfigurations)
        .build();
  }
}
