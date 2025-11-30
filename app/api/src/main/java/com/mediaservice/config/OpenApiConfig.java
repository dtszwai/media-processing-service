package com.mediaservice.config;

import io.swagger.v3.oas.models.OpenAPI;
import io.swagger.v3.oas.models.info.Info;
import io.swagger.v3.oas.models.servers.Server;
import io.swagger.v3.oas.models.Components;
import io.swagger.v3.oas.models.security.SecurityScheme;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;

import java.util.List;

@Configuration
public class OpenApiConfig {
  @Value("${server.port:9000}")
  private int serverPort;

  @Bean
  public OpenAPI customOpenAPI() {
    return new OpenAPI()
        .info(new Info()
            .title("Media Processing Service API")
            .version("1.0.0")
            .description("""
                REST API for media upload, processing, and management.

                ## Features
                - Direct file upload (up to 100MB)
                - Presigned URL upload (up to 5GB)
                - Image resizing with configurable dimensions
                - Multiple output formats (JPEG, PNG, WebP)
                - Async processing with status polling

                ## Rate Limits
                - General API: 100 requests/minute
                - Upload endpoints: 10 requests/minute
                """))
        .servers(List.of(
            new Server()
                .url("http://localhost:" + serverPort)
                .description("Local development server")))
        .components(new Components()
            .addSecuritySchemes("ApiKeyAuth", new SecurityScheme()
                .type(SecurityScheme.Type.APIKEY)
                .in(SecurityScheme.In.HEADER)
                .name("X-API-Key")
                .description("API key for authentication (if enabled)")));
  }
}
