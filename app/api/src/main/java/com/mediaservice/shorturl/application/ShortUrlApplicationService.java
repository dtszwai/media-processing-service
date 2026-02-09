package com.mediaservice.shorturl.application;

import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.media.application.MediaApplicationService;
import com.mediaservice.shorturl.api.dto.CreateShortUrlRequest;
import com.mediaservice.shorturl.domain.model.ShortUrl;
import com.mediaservice.shorturl.infrastructure.persistence.ShortUrlDynamoDbRepository;
import com.mediaservice.shared.auth.AuthorizationService;
import com.mediaservice.shared.config.properties.ShortUrlProperties;
import com.mediaservice.shared.http.error.MediaGoneException;
import java.security.SecureRandom;
import java.time.Instant;
import java.util.List;
import java.util.Locale;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.stereotype.Service;
import software.amazon.awssdk.services.dynamodb.model.ConditionalCheckFailedException;

@Slf4j
@Service
@RequiredArgsConstructor
public class ShortUrlApplicationService {
  private static final SecureRandom SECURE_RANDOM = new SecureRandom();
  private static final String ALPHABET = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz";
  private static final int MAX_GENERATION_ATTEMPTS = 5;

  private final ShortUrlDynamoDbRepository shortUrlRepository;
  private final MediaApplicationService mediaService;
  private final ShortUrlProperties shortUrlProperties;
  private final AuthorizationService authorizationService;

  public ShortUrl createShortUrl(CreateShortUrlRequest request, String tenantId, String userId) {
    authorizationService.requireAuthenticated();
    String mediaId = requireNonBlank(request.getMediaId(), "mediaId");
    String assetId = requireNonBlank(request.getAssetId(), "assetId");

    var media = mediaService.getActiveMedia(mediaId);
    if (media.isEmpty()) {
      throw new IllegalArgumentException("Media not found");
    }
    var asset = mediaService.getAsset(mediaId, assetId);
    if (asset.isEmpty() || asset.get().getStatus() == AssetStatus.DELETED) {
      throw new IllegalArgumentException("Asset not found");
    }

    Instant expiresAt = request.getExpiresAt();
    if (expiresAt != null && expiresAt.isBefore(Instant.now())) {
      throw new IllegalArgumentException("expiresAt must be in the future");
    }

    String alias = normalizeAlias(request.getAlias());
    if (alias != null) {
      validateAlias(alias);
    }

    var shortUrl = ShortUrl.builder()
        .code(alias)
        .tenantId(tenantId)
        .mediaId(mediaId)
        .assetId(assetId)
        .isPublic(true)
        .createdAt(Instant.now())
        .createdBy(userId)
        .expiresAt(expiresAt)
        .label(request.getLabel())
        .build();

    if (alias != null) {
      shortUrlRepository.createShortUrl(shortUrl);
      return shortUrl;
    }

    for (int attempt = 0; attempt < MAX_GENERATION_ATTEMPTS; attempt++) {
      shortUrl.setCode(generateCode(shortUrlProperties.getGeneratedLength()));
      try {
        shortUrlRepository.createShortUrl(shortUrl);
        return shortUrl;
      } catch (ConditionalCheckFailedException e) {
        log.debug("Short URL collision on code {}, retrying...", shortUrl.getCode());
      }
    }

    throw new IllegalStateException("Failed to generate unique short URL");
  }

  public ShortUrlResolveResult resolve(String code) {
    if (code == null || code.isBlank()) {
      return new ShortUrlResolveResult.NotFound();
    }
    String trimmedCode = code.trim();
    var shortUrlOpt = shortUrlRepository.getByCode(trimmedCode);
    if (shortUrlOpt.isEmpty()) {
      return new ShortUrlResolveResult.NotFound();
    }
    var shortUrl = shortUrlOpt.get();
    if (!shortUrl.isPublic()) {
      return new ShortUrlResolveResult.NotFound();
    }
    if (shortUrl.getRevokedAt() != null) {
      return new ShortUrlResolveResult.Gone("revoked");
    }
    if (shortUrl.getExpiresAt() != null && !shortUrl.getExpiresAt().isAfter(Instant.now())) {
      return new ShortUrlResolveResult.Gone("expired");
    }

    try {
      return resolveByAsset(shortUrl);
    } catch (MediaGoneException e) {
      return new ShortUrlResolveResult.Gone("deleted");
    }
  }

  public ShortUrl getShortUrlForTenant(String code, String tenantId) {
    authorizationService.requireAuthenticated();
    if (code == null || code.isBlank()) {
      return null;
    }
    var shortUrlOpt = shortUrlRepository.getByCode(code.trim());
    if (shortUrlOpt.isEmpty()) {
      return null;
    }
    var shortUrl = shortUrlOpt.get();
    if (!tenantId.equals(shortUrl.getTenantId())) {
      return null;
    }
    return shortUrl;
  }

  public boolean revokeShortUrl(String code, String tenantId) {
    var shortUrl = getShortUrlForTenant(code, tenantId);
    if (shortUrl == null) {
      return false;
    }
    shortUrlRepository.revokeShortUrl(shortUrl.getCode(), Instant.now(), shortUrl.getMediaId());
    return true;
  }

  public List<ShortUrl> listShortUrls(String mediaId, String tenantId, Integer limit) {
    authorizationService.requireAuthenticated();
    var results = shortUrlRepository.listByMedia(mediaId, limit);
    return results.stream()
        .filter(url -> tenantId.equals(url.getTenantId()))
        .toList();
  }

  private ShortUrlResolveResult resolveByAsset(ShortUrl shortUrl) {
    String assetId = shortUrl.getAssetId();
    if (assetId == null || assetId.isBlank()) {
      return new ShortUrlResolveResult.NotFound();
    }

    var assetOpt = mediaService.getAsset(shortUrl.getMediaId(), assetId);
    if (assetOpt.isEmpty() || assetOpt.get().getStatus() == AssetStatus.DELETED) {
      return new ShortUrlResolveResult.NotFound();
    }

    var asset = assetOpt.get();
    if (asset.getStatus() != AssetStatus.COMPLETE) {
      return new ShortUrlResolveResult.Processing(shortUrl.getMediaId());
    }

    var urlOpt = mediaService.getAssetDownloadUrlPublic(shortUrl.getMediaId(), assetId);
    return urlOpt
        .map(url -> (ShortUrlResolveResult) new ShortUrlResolveResult.Ready(url))
        .orElseGet(ShortUrlResolveResult.NotFound::new);
  }

  private String generateCode(int length) {
    int resolvedLength = Math.max(4, length);
    var chars = new char[resolvedLength];
    for (int i = 0; i < resolvedLength; i++) {
      chars[i] = ALPHABET.charAt(SECURE_RANDOM.nextInt(ALPHABET.length()));
    }
    return new String(chars);
  }

  private String normalizeAlias(String alias) {
    if (alias == null || alias.isBlank()) {
      return null;
    }
    return alias.trim().toLowerCase(Locale.ROOT);
  }

  private String requireNonBlank(String value, String fieldName) {
    if (value == null || value.isBlank()) {
      throw new IllegalArgumentException(fieldName + " is required");
    }
    return value;
  }

  private void validateAlias(String alias) {
    int min = shortUrlProperties.getMinAliasLength();
    int max = shortUrlProperties.getMaxAliasLength();
    if (alias.length() < min || alias.length() > max) {
      throw new IllegalArgumentException(
          "alias length must be between %d and %d characters".formatted(min, max));
    }
    if (isReserved(alias)) {
      throw new IllegalArgumentException("alias is reserved");
    }
  }

  private boolean isReserved(String alias) {
    if (shortUrlProperties.getReservedAliases() == null) {
      return false;
    }
    return shortUrlProperties.getReservedAliases().stream()
        .filter(value -> value != null && !value.isBlank())
        .anyMatch(value -> value.trim().equalsIgnoreCase(alias));
  }
}
