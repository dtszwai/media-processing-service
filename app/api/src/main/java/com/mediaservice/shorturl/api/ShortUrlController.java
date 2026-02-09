package com.mediaservice.shorturl.api;

import com.mediaservice.shared.auth.TenantContext;
import com.mediaservice.shared.config.properties.ShortUrlProperties;
import com.mediaservice.shorturl.api.dto.CreateShortUrlRequest;
import com.mediaservice.shorturl.api.dto.ShortUrlResponse;
import com.mediaservice.shorturl.application.ShortUrlApplicationService;
import com.mediaservice.shorturl.domain.model.ShortUrl;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.servlet.http.HttpServletRequest;
import jakarta.validation.Valid;
import java.net.URI;
import java.util.List;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RequestParam;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequestMapping("/v1/short-urls")
@RequiredArgsConstructor
@Tag(name = "Short URLs", description = "Short URL management endpoints")
public class ShortUrlController {
  private final ShortUrlApplicationService shortUrlService;
  private final ShortUrlProperties shortUrlProperties;

  @Operation(summary = "Create a short URL")
  @PostMapping
  public ResponseEntity<ShortUrlResponse> createShortUrl(
      @Valid @RequestBody CreateShortUrlRequest request,
      HttpServletRequest httpServletRequest) {
    String tenantId = TenantContext.getTenantId();
    String userId = TenantContext.getUserId();
    ShortUrl shortUrl = shortUrlService.createShortUrl(request, tenantId, userId);
    ShortUrlResponse response = toResponse(shortUrl, httpServletRequest);
    return ResponseEntity.created(URI.create("/v1/short-urls/" + shortUrl.getCode())).body(response);
  }

  @Operation(summary = "Get a short URL")
  @GetMapping("/{code}")
  public ResponseEntity<ShortUrlResponse> getShortUrl(
      @PathVariable String code,
      HttpServletRequest httpServletRequest) {
    String tenantId = TenantContext.getTenantId();
    ShortUrl shortUrl = shortUrlService.getShortUrlForTenant(code, tenantId);
    if (shortUrl == null) {
      return ResponseEntity.notFound().build();
    }
    return ResponseEntity.ok(toResponse(shortUrl, httpServletRequest));
  }

  @Operation(summary = "List short URLs for a media item")
  @GetMapping
  public ResponseEntity<List<ShortUrlResponse>> listShortUrls(
      @RequestParam String mediaId,
      @RequestParam(required = false) Integer limit,
      HttpServletRequest httpServletRequest) {
    if (mediaId == null || mediaId.isBlank()) {
      return ResponseEntity.badRequest().build();
    }
    String tenantId = TenantContext.getTenantId();
    var results = shortUrlService.listShortUrls(mediaId, tenantId, limit);
    var response = results.stream()
        .map(shortUrl -> toResponse(shortUrl, httpServletRequest))
        .toList();
    return ResponseEntity.ok(response);
  }

  @Operation(summary = "Revoke a short URL")
  @DeleteMapping("/{code}")
  public ResponseEntity<Void> revokeShortUrl(@PathVariable String code) {
    String tenantId = TenantContext.getTenantId();
    boolean revoked = shortUrlService.revokeShortUrl(code, tenantId);
    return revoked ? ResponseEntity.noContent().build() : ResponseEntity.notFound().build();
  }

  private ShortUrlResponse toResponse(ShortUrl shortUrl, HttpServletRequest request) {
    String baseDomain = resolveBaseDomain(request);
    String shortUrlValue = baseDomain != null ? baseDomain + "/s/" + shortUrl.getCode() : null;
    return ShortUrlResponse.builder()
        .code(shortUrl.getCode())
        .shortUrl(shortUrlValue)
        .mediaId(shortUrl.getMediaId())
        .assetId(shortUrl.getAssetId())
        .isPublic(shortUrl.isPublic())
        .createdAt(shortUrl.getCreatedAt())
        .createdBy(shortUrl.getCreatedBy())
        .expiresAt(shortUrl.getExpiresAt())
        .revokedAt(shortUrl.getRevokedAt())
        .label(shortUrl.getLabel())
        .build();
  }

  private String resolveBaseDomain(HttpServletRequest request) {
    String configured = shortUrlProperties.getBaseDomain();
    if (configured != null && !configured.isBlank()) {
      return trimTrailingSlash(configured.trim());
    }

    String scheme = headerOrDefault(request, "X-Forwarded-Proto", request.getScheme());
    String host = headerOrDefault(request, "X-Forwarded-Host", request.getServerName());
    String portHeader = headerOrDefault(request, "X-Forwarded-Port", String.valueOf(request.getServerPort()));
    String base = scheme + "://" + host;
    if (!host.contains(":")) {
      int port = safeParsePort(portHeader, request.getServerPort());
      if (!isDefaultPort(scheme, port)) {
        base += ":" + port;
      }
    }
    return base;
  }

  private String headerOrDefault(HttpServletRequest request, String header, String defaultValue) {
    String value = request.getHeader(header);
    if (value == null || value.isBlank()) {
      return defaultValue;
    }
    String[] parts = value.split(",");
    return parts[0].trim();
  }

  private boolean isDefaultPort(String scheme, int port) {
    return ("http".equalsIgnoreCase(scheme) && port == 80) ||
        ("https".equalsIgnoreCase(scheme) && port == 443);
  }

  private int safeParsePort(String value, int fallback) {
    try {
      return Integer.parseInt(value);
    } catch (NumberFormatException e) {
      return fallback;
    }
  }

  private String trimTrailingSlash(String value) {
    if (value == null) {
      return null;
    }
    return value.endsWith("/") ? value.substring(0, value.length() - 1) : value;
  }
}
