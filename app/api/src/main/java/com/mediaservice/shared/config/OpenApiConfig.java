package com.mediaservice.shared.config;

import com.mediaservice.shared.auth.AuthProperties;
import com.mediaservice.shared.idempotency.Idempotent;
import io.swagger.v3.oas.models.Components;
import io.swagger.v3.oas.models.Operation;
import io.swagger.v3.oas.models.OpenAPI;
import io.swagger.v3.oas.models.headers.Header;
import io.swagger.v3.oas.models.info.Info;
import io.swagger.v3.oas.models.media.StringSchema;
import io.swagger.v3.oas.models.parameters.Parameter;
import io.swagger.v3.oas.models.responses.ApiResponse;
import io.swagger.v3.oas.models.servers.Server;
import io.swagger.v3.oas.models.security.SecurityRequirement;
import io.swagger.v3.oas.models.security.SecurityScheme;
import org.springdoc.core.customizers.OperationCustomizer;
import org.springframework.core.annotation.AnnotatedElementUtils;
import org.springframework.beans.factory.annotation.Value;
import org.springframework.boot.info.BuildProperties;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import org.springframework.lang.Nullable;
import org.springframework.web.method.HandlerMethod;

import java.util.List;

@Configuration
public class OpenApiConfig {
  @Value("${server.port:9000}")
  private int serverPort;

  private final BuildProperties buildProperties;

  public OpenApiConfig(@Nullable BuildProperties buildProperties) {
    this.buildProperties = buildProperties;
  }

  @Bean
  public OpenAPI customOpenAPI() {
    String version = buildProperties != null ? buildProperties.getVersion() : "1.0.0";
    return new OpenAPI()
        .info(new Info()
            .title("Media Processing Service API")
            .version(version)
            .description("""
                REST API for media upload, processing, and management.

                ## Features
                - Direct file upload (up to 50MB)
                - Presigned URL upload for large files (up to 1GB)
                - Image resizing with configurable dimensions (max 8192px)
                - Multiple output formats (JPEG, PNG, WebP)
                - Async processing with status polling
                - Multi-tenant with JWT and API key authentication

                ## Authentication
                - **JWT Bearer**: Register via `/v1/auth/register`, login via `/v1/auth/login`
                - **API Key**: Create via `/v1/auth/api-keys`, pass in `X-API-Key` header

                ## Limits
                - Direct upload: 50MB max
                - Presigned upload: 1GB max
                - Max image dimension: 8192x8192px
                - General API rate limit: 100 requests/minute
                - Upload rate limit: 10 requests/minute
                """))
        .servers(List.of(
            new Server()
                .url("http://localhost:" + serverPort)
                .description("Local development server")))
        .addSecurityItem(new SecurityRequirement().addList("BearerAuth"))
        .addSecurityItem(new SecurityRequirement().addList("ApiKeyAuth"))
        .components(new Components()
            .addSecuritySchemes("BearerAuth", new SecurityScheme()
                .type(SecurityScheme.Type.HTTP)
                .scheme("bearer")
                .bearerFormat("JWT")
                .description("JWT access token from /v1/auth/login"))
            .addSecuritySchemes("ApiKeyAuth", new SecurityScheme()
                .type(SecurityScheme.Type.APIKEY)
                .in(SecurityScheme.In.HEADER)
                .name("X-API-Key")
                .description("API key from /v1/auth/api-keys")));
  }

  @Bean
  public OperationCustomizer headerOperationCustomizer(AuthProperties authProperties) {
    return (Operation operation, HandlerMethod handlerMethod) -> {
      addHeaderParameter(operation,
          "Authorization",
          "Bearer access token (e.g., \"Bearer <jwt>\") if using JWT auth.",
          false);

      addHeaderParameter(operation,
          authProperties.getApiKey().getHeader(),
          "API key for authentication (optional if using Authorization header).",
          false);

      if (AnnotatedElementUtils.hasAnnotation(handlerMethod.getMethod(), Idempotent.class)) {
        addHeaderParameter(operation,
            "Idempotency-Key",
            "Optional idempotency key to safely retry requests.",
            false);
      }

      addResponseHeader(operation, "X-Request-ID",
          "Request ID for tracing (echoed by the server).");
      addResponseHeader(operation, "X-Correlation-ID",
          "Correlation ID for cross-service tracing (echoed by the server).");
      addResponseHeader(operation, "X-Rate-Limit-Remaining",
          "Remaining requests in the current rate limit window (when rate limiting is enabled).");
      addResponseHeader(operation, "X-Content-Type-Options",
          "Security header to prevent MIME type sniffing.");
      addResponseHeader(operation, "X-Frame-Options",
          "Clickjacking protection (DENY or SAMEORIGIN for Swagger UI).");
      addResponseHeader(operation, "X-XSS-Protection",
          "Legacy XSS protection header for older browsers.");

      addRateLimit429Response(operation);

      return operation;
    };
  }

  private static void addHeaderParameter(Operation operation, String name, String description, boolean required) {
    if (operation.getParameters() != null) {
      boolean alreadyPresent = operation.getParameters().stream()
          .anyMatch(parameter -> "header".equalsIgnoreCase(parameter.getIn())
              && name.equalsIgnoreCase(parameter.getName()));
      if (alreadyPresent) {
        return;
      }
    }

    Parameter header = new Parameter()
        .in("header")
        .name(name)
        .description(description)
        .required(required)
        .schema(new StringSchema());

    operation.addParametersItem(header);
  }

  private static void addResponseHeader(Operation operation, String name, String description) {
    if (operation.getResponses() == null || operation.getResponses().isEmpty()) {
      return;
    }

    Header header = new Header()
        .description(description)
        .schema(new StringSchema());

    for (ApiResponse response : operation.getResponses().values()) {
      if (response.getHeaders() != null && response.getHeaders().containsKey(name)) {
        continue;
      }
      response.addHeaderObject(name, header);
    }
  }

  private static void addRateLimit429Response(Operation operation) {
    if (operation.getResponses() == null) {
      return;
    }

    if (operation.getResponses().containsKey("429")) {
      return;
    }

    ApiResponse rateLimitResponse = new ApiResponse()
        .description("Rate limit exceeded.");
    rateLimitResponse.addHeaderObject(
        "X-Rate-Limit-Retry-After-Seconds",
        new Header()
            .description("Seconds until the rate limit resets.")
            .schema(new StringSchema()));

    operation.getResponses().addApiResponse("429", rateLimitResponse);
  }
}
