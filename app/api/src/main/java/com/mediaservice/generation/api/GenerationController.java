package com.mediaservice.generation.api;

import com.mediaservice.common.generation.GenerationOutputType;
import com.mediaservice.common.generation.GenerationStatus;
import com.mediaservice.generation.api.dto.CreateAudioOverviewRequest;
import com.mediaservice.generation.api.dto.CreateGenerationRequest;
import com.mediaservice.generation.api.dto.GenerationResponse;
import com.mediaservice.generation.api.dto.GenerationResultResponse;
import com.mediaservice.generation.application.GenerationApplicationService;
import com.mediaservice.shared.auth.TenantContext;
import com.mediaservice.shared.idempotency.Idempotent;
import jakarta.validation.Valid;
import java.util.List;
import java.util.Map;
import lombok.RequiredArgsConstructor;
import lombok.extern.slf4j.Slf4j;
import org.springframework.http.HttpStatus;
import org.springframework.http.ResponseEntity;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PathVariable;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

@Slf4j
@RestController
@RequestMapping("/v1")
@RequiredArgsConstructor
public class GenerationController {
  private final GenerationApplicationService generationService;

  @Idempotent(scope = "create-generation")
  @PostMapping("/generations")
  public ResponseEntity<GenerationResponse> createGeneration(@Valid @RequestBody CreateGenerationRequest request) {
    log.info("Generation request received: tenant={}, resolution={}", TenantContext.getTenantId(), request.getResolution());
    var job = generationService.submitImage(request);
    return ResponseEntity.status(HttpStatus.ACCEPTED).body(toResponse(job));
  }

  @Idempotent(scope = "create-audio-overview")
  @PostMapping("/audio-overviews")
  public ResponseEntity<GenerationResponse> createAudioOverview(@Valid @RequestBody CreateAudioOverviewRequest request) {
    log.info("Audio overview request received: tenant={}, provider={}", TenantContext.getTenantId(), request.getProvider());
    var job = generationService.submitAudioOverview(request);
    return ResponseEntity.status(HttpStatus.ACCEPTED).body(toResponse(job));
  }

  @GetMapping("/generations/{jobId}")
  public ResponseEntity<GenerationResponse> getGeneration(@PathVariable String jobId) {
    return generationService.getJob(jobId)
        .map(job -> ResponseEntity.ok(toResponse(job)))
        .orElse(ResponseEntity.notFound().build());
  }

  @GetMapping("/generations/{jobId}/result")
  public ResponseEntity<GenerationResultResponse> getGenerationResult(@PathVariable String jobId) {
    var job = generationService.getJob(jobId);
    if (job.isEmpty()) {
      return ResponseEntity.notFound().build();
    }
    if (job.get().getStatus() != GenerationStatus.COMPLETE) {
      return ResponseEntity.status(HttpStatus.ACCEPTED)
          .body(GenerationResultResponse.builder()
              .jobId(job.get().getJobId())
              .mediaId(job.get().getMediaId())
              .status(job.get().getStatus())
              .aiGenerated(job.get().getAiGenerated())
              .build());
    }
    return generationService.getResult(jobId)
        .map(result -> ResponseEntity.ok(GenerationResultResponse.builder()
            .jobId(result.job().getJobId())
            .mediaId(result.job().getMediaId())
            .status(result.job().getStatus())
            .imageUrl(result.job().getOutputType() == GenerationOutputType.IMAGE ? result.url() : null)
            .audioUrl(result.job().getOutputType() == GenerationOutputType.AUDIO ? result.url() : null)
            .expiresAt(result.expiresAt())
            .variants(List.of("original"))
            .aiGenerated(result.job().getAiGenerated())
            .build()))
        .orElse(ResponseEntity.status(HttpStatus.ACCEPTED).build());
  }

  private GenerationResponse toResponse(com.mediaservice.common.generation.GenerationJob job) {
    GenerationResponse.AcceptedConfig acceptedConfig = new GenerationResponse.AcceptedConfig(
        job.getResolution(),
        job.getEnhancedPrompt() != null && !job.getEnhancedPrompt().isBlank());

    GenerationResponse.Admission admission = buildAdmission(job);

    return GenerationResponse.builder()
        .jobId(job.getJobId())
        .mediaId(job.getMediaId())
        .status(job.getStatus())
        .stage(job.getCurrentStage())
        .estimatedWaitSeconds(job.getEstimatedWaitSeconds())
        .acceptedConfig(acceptedConfig)
        .admission(admission)
        .createdAt(job.getCreatedAt())
        .updatedAt(job.getUpdatedAt())
        .errorCode(job.getErrorCode())
        .errorMessage(userFriendlyError(job.getErrorCode()))
        .aiGenerated(job.getAiGenerated())
        .build();
  }

  private GenerationResponse.Admission buildAdmission(com.mediaservice.common.generation.GenerationJob job) {
    Map<String, String> metadata = job.getMetadata();
    String tier = job.getTier() != null ? job.getTier() : "free";
    if (metadata == null || metadata.isEmpty()) {
      return new GenerationResponse.Admission(tier, "ACCEPTED", "ADMITTED", null);
    }
    Integer retryAfter = parseIntOrNull(metadata.get("retry_after_seconds"));
    return new GenerationResponse.Admission(
        tier,
        metadata.getOrDefault("admission_decision", "ACCEPTED"),
        metadata.getOrDefault("admission_code", "ADMITTED"),
        retryAfter);
  }

  private Integer parseIntOrNull(String value) {
    if (value == null || value.isBlank()) {
      return null;
    }
    try {
      return Integer.parseInt(value);
    } catch (NumberFormatException e) {
      return null;
    }
  }

  private static String userFriendlyError(String errorCode) {
    if (errorCode == null) {
      return null;
    }
    return switch (errorCode) {
      case "BUDGET_EXCEEDED" -> "Daily budget exceeded. Try again tomorrow.";
      case "MODERATION_REJECTED", "MODERATION_BLOCKED" -> "Prompt rejected by safety review.";
      case "PROVIDER_TIMEOUT" -> "Generation provider timed out.";
      default -> "Generation failed.";
    };
  }
}
