package com.mediaservice.shorturl.application;

import com.mediaservice.common.model.ShortUrlVariant;
import com.mediaservice.media.application.DownloadResult;
import com.mediaservice.media.application.MediaApplicationService;
import com.mediaservice.media.application.PreviewResult;
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
    ShortUrlVariant variant = ShortUrlVariant.fromString(request.getVariant());
    if (variant == null) {
      throw new IllegalArgumentException("variant must be one of: preview, download, original");
    }
    if (request.getMediaId() == null || request.getMediaId().isBlank()) {
      throw new IllegalArgumentException("mediaId is required");
    }

    var media = mediaService.getActiveMedia(request.getMediaId());
    if (media.isEmpty()) {
      throw new IllegalArgumentException("Media not found");
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
        .mediaId(request.getMediaId())
        .variant(variant)
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

    if (shortUrl.getVariant() == null) {
      return new ShortUrlResolveResult.NotFound();
    }

    try {
      return switch (shortUrl.getVariant()) {
        case PREVIEW -> resolvePreview(shortUrl);
        case DOWNLOAD -> resolveDownload(shortUrl);
        case ORIGINAL -> resolveOriginal(shortUrl);
      };
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

  private ShortUrlResolveResult resolvePreview(ShortUrl shortUrl) {
    return switch (mediaService.preparePreviewPublic(shortUrl.getMediaId())) {
      case PreviewResult.Ready ready -> new ShortUrlResolveResult.Ready(ready.url());
      case PreviewResult.Processing processing -> new ShortUrlResolveResult.Processing(processing.mediaId());
      case PreviewResult.NotFound ignored -> new ShortUrlResolveResult.NotFound();
    };
  }

  private ShortUrlResolveResult resolveDownload(ShortUrl shortUrl) {
    return switch (mediaService.prepareDownloadPublic(shortUrl.getMediaId())) {
      case DownloadResult.Ready ready -> new ShortUrlResolveResult.Ready(ready.url());
      case DownloadResult.Processing processing -> new ShortUrlResolveResult.Processing(processing.mediaId());
      case DownloadResult.NotFound ignored -> new ShortUrlResolveResult.NotFound();
    };
  }

  private ShortUrlResolveResult resolveOriginal(ShortUrl shortUrl) {
    var urlOpt = mediaService.getOriginalUrlPublic(shortUrl.getMediaId());
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
