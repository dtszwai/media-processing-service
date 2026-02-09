package com.mediaservice.auth.application;

import com.mediaservice.auth.api.dto.ApiKeyResponse;
import com.mediaservice.auth.domain.model.ApiKey;
import com.mediaservice.auth.infrastructure.persistence.ApiKeyDynamoDbRepository;
import com.mediaservice.shared.auth.ApiKeyService;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;

import java.security.SecureRandom;
import java.time.Instant;
import java.util.Base64;
import java.util.List;

@Slf4j
@Service
@RequiredArgsConstructor
public class ApiKeyManagementService {
  private static final SecureRandom SECURE_RANDOM = new SecureRandom();
  private static final int RAW_KEY_BYTES = 32;

  private final ApiKeyDynamoDbRepository apiKeyRepository;

  public ApiKeyResponse createApiKey(String tenantId, String name) {
    // Generate raw key
    byte[] keyBytes = new byte[RAW_KEY_BYTES];
    SECURE_RANDOM.nextBytes(keyBytes);
    String rawKey = Base64.getUrlEncoder().withoutPadding().encodeToString(keyBytes);
    Instant now = Instant.now();

    String keyPrefix = ApiKeyService.extractPrefix(rawKey);
    String hashedKey = ApiKeyService.hashKey(rawKey);

    apiKeyRepository.createApiKey(ApiKey.builder()
        .keyId(keyPrefix)
        .tenantId(tenantId)
        .hashedKey(hashedKey)
        .name(name)
        .scopes(List.of("USER"))
        .createdAt(now)
        .build());

    log.info("Created API key '{}' for tenant: {}", name, tenantId);

    return ApiKeyResponse.builder()
        .keyId(keyPrefix)
        .rawKey(rawKey)
        .name(name)
        .createdAt(now)
        .build();
  }

  public List<ApiKeyResponse> listApiKeys(String tenantId) {
    return apiKeyRepository.getApiKeysByTenant(tenantId).stream()
        .map(key -> ApiKeyResponse.builder()
            .keyId(key.getKeyId())
            .name(key.getName())
            .createdAt(key.getCreatedAt())
            .build())
        .toList();
  }

  public boolean revokeApiKey(String tenantId, String keyId) {
    var existing = apiKeyRepository.getApiKey(keyId);
    if (existing.isEmpty() || !existing.get().getTenantId().equals(tenantId)) {
      return false;
    }
    apiKeyRepository.deleteApiKey(keyId);
    log.info("Revoked API key '{}' for tenant: {}", keyId, tenantId);
    return true;
  }
}
