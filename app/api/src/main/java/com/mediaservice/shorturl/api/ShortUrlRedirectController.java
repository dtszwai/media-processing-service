package com.mediaservice.shorturl.api;

import com.mediaservice.shorturl.application.ShortUrlApplicationService;
import com.mediaservice.shorturl.application.ShortUrlResolveResult;
import io.swagger.v3.oas.annotations.Operation;
import io.swagger.v3.oas.annotations.tags.Tag;
import jakarta.servlet.http.HttpServletRequest;
import java.net.URI;
import lombok.RequiredArgsConstructor;
import org.springframework.http.HttpHeaders;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.RestController;

@RestController
@RequiredArgsConstructor
@Tag(name = "Short URLs", description = "Short URL resolve endpoint")
public class ShortUrlRedirectController {
  private final ShortUrlApplicationService shortUrlService;

  @Operation(summary = "Resolve short URL")
  @GetMapping("/s/{code}")
  public ResponseEntity<Void> resolveShortUrl(@PathVariable String code, HttpServletRequest request) {
    ShortUrlResolveResult result = shortUrlService.resolve(code);
    return switch (result) {
      case ShortUrlResolveResult.Ready ready ->
          ResponseEntity.status(HttpStatus.FOUND).location(URI.create(ready.url())).build();
      case ShortUrlResolveResult.Processing processing -> {
        var headers = new HttpHeaders();
        headers.add("Retry-After", "60");
        headers.add("Location", "%s://%s:%d/v1/media/%s/status"
            .formatted(request.getScheme(), request.getServerName(), request.getServerPort(), processing.mediaId()));
        yield ResponseEntity.accepted().headers(headers).build();
      }
      case ShortUrlResolveResult.Gone ignored -> ResponseEntity.status(HttpStatus.GONE).build();
      case ShortUrlResolveResult.NotFound ignored -> ResponseEntity.notFound().build();
    };
  }
}
