package com.mediaservice.shared.config;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.web.cors.CorsConfiguration;
import org.springframework.web.cors.UrlBasedCorsConfigurationSource;
import org.springframework.web.filter.CorsFilter;

import java.util.List;

@Configuration
public class CorsConfig {
  @Value("${cors.allowed-origins:*}")
  private String allowedOrigins;

  @Bean
  public CorsFilter corsFilter() {
    CorsConfiguration config = new CorsConfiguration();

    if ("*".equals(allowedOrigins)) {
      config.setAllowCredentials(false);
      config.addAllowedOriginPattern("*");
    } else {
      config.setAllowCredentials(true);
      config.setAllowedOrigins(List.of(allowedOrigins.split(",")));
    }

    config.addAllowedHeader("*");
    config.addAllowedMethod("*");
    config.addExposedHeader("Location");
    config.addExposedHeader("Authorization");
    UrlBasedCorsConfigurationSource source = new UrlBasedCorsConfigurationSource();
    source.registerCorsConfiguration("/**", config);
    return new CorsFilter(source);
  }
}
