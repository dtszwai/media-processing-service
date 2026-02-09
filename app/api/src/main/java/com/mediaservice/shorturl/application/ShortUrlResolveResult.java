package com.mediaservice.shorturl.application;

public sealed interface ShortUrlResolveResult {
  record Ready(String url) implements ShortUrlResolveResult {}
  record Processing(String mediaId) implements ShortUrlResolveResult {}
  record Gone(String reason) implements ShortUrlResolveResult {}
  record NotFound() implements ShortUrlResolveResult {}
}
