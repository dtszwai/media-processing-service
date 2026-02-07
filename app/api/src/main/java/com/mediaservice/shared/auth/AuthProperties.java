package com.mediaservice.shared.auth;

import lombok.Data;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.stereotype.Component;

@Data
@Component
@ConfigurationProperties(prefix = "auth")
public class AuthProperties {
  private Jwt jwt = new Jwt();
  private ApiKey apiKey = new ApiKey();
  private Enforcement enforcement = new Enforcement();

  @Data
  public static class Jwt {
    private String secret = "local-dev-secret-min-32-chars-long!!";
    private String issuer = "media-service";
    private long expirationSeconds = 3600;
    private long refreshExpirationSeconds = 86400;
  }

  @Data
  public static class ApiKey {
    private boolean enabled = true;
    private String header = "X-API-Key";
  }

  @Data
  public static class Enforcement {
    private boolean enabled = false;
  }
}
