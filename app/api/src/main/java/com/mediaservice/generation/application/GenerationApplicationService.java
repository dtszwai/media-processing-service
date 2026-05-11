package com.mediaservice.generation.application;

import com.mediaservice.common.generation.GenerationJob;
import com.mediaservice.common.generation.GenerationOutputType;
import com.mediaservice.generation.api.dto.CreateAudioOverviewRequest;
import com.mediaservice.generation.api.dto.CreateGenerationRequest;
import com.mediaservice.providers.generation.core.GenerationSubmission;
import com.mediaservice.providers.generation.core.GenerationWorkflow;
import com.mediaservice.shared.auth.TenantContext;
import java.util.Optional;
import lombok.RequiredArgsConstructor;
import org.springframework.stereotype.Service;

@Service
@RequiredArgsConstructor
public class GenerationApplicationService {
  private final GenerationWorkflow workflow;

  public GenerationJob submitImage(CreateGenerationRequest request) {
    return workflow.submit(new GenerationSubmission(
        TenantContext.getTenantId(),
        TenantContext.getUserId(),
        request.getTier(),
        GenerationOutputType.IMAGE,
        request.getPrompt(),
        request.getModel(),
        request.getResolution(),
        request.getSeed(),
        request.getWebhookUrl(),
        null));
  }

  public GenerationJob submitAudioOverview(CreateAudioOverviewRequest request) {
    return workflow.submit(new GenerationSubmission(
        TenantContext.getTenantId(),
        TenantContext.getUserId(),
        request.getTier(),
        GenerationOutputType.AUDIO,
        request.getTopic(),
        null,
        null,
        null,
        request.getWebhookUrl(),
        request.getProvider()));
  }

  public Optional<GenerationJob> getJob(String jobId) {
    return workflow.getJob(jobId)
        .filter(job -> TenantContext.getTenantId().equals(job.getTenantId()));
  }

  public Optional<GenerationWorkflow.ResultView> getResult(String jobId) {
    return workflow.getJob(jobId)
        .filter(job -> TenantContext.getTenantId().equals(job.getTenantId()))
        .flatMap(job -> workflow.result(jobId));
  }
}
