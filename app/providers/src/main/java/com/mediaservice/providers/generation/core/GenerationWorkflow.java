package com.mediaservice.providers.generation.core;

import com.mediaservice.common.generation.GenerationErrorCode;

import com.mediaservice.common.constants.StorageConstants;
import com.mediaservice.common.generation.GenerationArtifact;
import com.mediaservice.common.generation.GenerationJob;
import com.mediaservice.common.generation.GenerationOutputType;
import com.mediaservice.common.generation.GenerationStage;
import com.mediaservice.common.generation.GenerationStageMessage;
import com.mediaservice.common.generation.GenerationStatus;
import com.mediaservice.common.generation.Tier;
import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.providers.generation.audio.AudioOverviewProvider;
import com.mediaservice.providers.generation.image.ImageProvider;
import com.mediaservice.common.generation.provider.JobSpec;
import com.mediaservice.common.generation.provider.ProviderKind;
import com.mediaservice.common.generation.provider.ProviderJobId;
import com.mediaservice.common.generation.provider.ProviderStatus;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.AssetType;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.common.model.MediaSource;
import com.mediaservice.common.model.MediaStatus;
import com.mediaservice.common.model.MediaType;
import java.security.MessageDigest;
import java.time.Duration;
import java.time.Instant;
import java.util.HexFormat;
import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.Optional;
import java.util.UUID;
import lombok.extern.slf4j.Slf4j;
import com.mediaservice.providers.generation.audio.NotebookLmAudioOverviewProvider;
import com.mediaservice.providers.generation.image.ImageWatermarker;
import com.mediaservice.providers.generation.prompt.EnhancedPrompt;

@Slf4j
public class GenerationWorkflow {
  private static final List<String> ORIGINAL_TAGS = List.of("original", "generated");

  private final GenerationRepository repository;
  private final GenerationProviderFactory providerFactory;
  private final GeneratedAssetStorage storage;
  private final GenerationEventPublisher publisher;
  private final WebhookNotifier webhookNotifier;
  private final GenerationAdmissionController admissionController;
  private final GenerationMetrics metrics;

  public GenerationWorkflow(GenerationRepository repository, GenerationProviderFactory providerFactory,
      GeneratedAssetStorage storage, GenerationEventPublisher publisher, WebhookNotifier webhookNotifier) {
    this(repository, providerFactory, storage, publisher, webhookNotifier,
        GenerationAdmissionController.allowAll(), GenerationMetrics.noop());
  }

  public GenerationWorkflow(GenerationRepository repository, GenerationProviderFactory providerFactory,
      GeneratedAssetStorage storage, GenerationEventPublisher publisher, WebhookNotifier webhookNotifier,
      GenerationAdmissionController admissionController, GenerationMetrics metrics) {
    this.repository = repository;
    this.providerFactory = providerFactory;
    this.storage = storage;
    this.publisher = publisher;
    this.webhookNotifier = webhookNotifier;
    this.admissionController = admissionController;
    this.metrics = metrics;
  }

  public GenerationJob submit(GenerationSubmission submission) {
    String tier = normalizeTier(submission.tier());
    AdmissionVerdict verdict = evaluateAdmission(submission, tier);
    preflightAudio(submission);
    SubmissionArtifacts artifacts = buildSubmissionArtifacts(submission, verdict, tier);
    return persistAndPublish(artifacts, tier);
  }

  private AdmissionVerdict evaluateAdmission(GenerationSubmission submission, String tier) {
    AdmissionVerdict verdict = admissionController.evaluate(submission);
    metrics.recordAdmissionVerdict(submission.tenantId(), tier, verdict.decision().name(), verdict.code());
    if (!verdict.allowed()) {
      metrics.recordAdmissionRejected(submission.tenantId(), verdict.code());
      repository.recordAuditEvent(submission.tenantId(), null, "admission", verdict.code(),
          "admission-controller", null, "rejected", verdict.message());
      throw new GenerationProviderException(verdict.code(), verdict.message());
    }
    return verdict;
  }

  /**
   * Black-box pre-flight: any AudioOverviewProvider may report a credential problem (NotebookLM
   * session cookies, third-party API quota, etc.) without the workflow knowing the specifics.
   * Rejecting here saves a budget reservation, SQS message, and the user's wait time on a doomed job.
   */
  private void preflightAudio(GenerationSubmission submission) {
    if (submission.outputType() != GenerationOutputType.AUDIO) {
      return;
    }
    String audioProviderName = submission.audioOverviewProvider() != null
        && !submission.audioOverviewProvider().isBlank()
            ? submission.audioOverviewProvider()
            : providerFactory.config().audioOverviewProvider();
    AudioOverviewProvider.AuthHealth health = providerFactory.audioOverviewProvider(audioProviderName).health();
    if (health == AudioOverviewProvider.AuthHealth.OK) {
      return;
    }
    String code = health == AudioOverviewProvider.AuthHealth.AUTH_MISSING
        ? "NOTEBOOKLM_AUTH_MISSING"
        : "NOTEBOOKLM_AUTH_EXPIRED";
    throw new GenerationProviderException(code,
        "Audio overview provider not ready (" + health + "). Refresh credentials and retry.");
  }

  private SubmissionArtifacts buildSubmissionArtifacts(GenerationSubmission submission,
      AdmissionVerdict verdict, String tier) {
    String jobId = prefixedId(submission.outputType() == GenerationOutputType.AUDIO ? "ago" : "gen");
    String mediaId = UUID.randomUUID().toString();
    String assetId = UUID.randomUUID().toString();
    Instant now = Instant.now();
    String model = submission.model() != null && !submission.model().isBlank()
        ? submission.model()
        : providerFactory.config().model();
    MediaType mediaType = submission.outputType() == GenerationOutputType.AUDIO ? MediaType.AUDIO : MediaType.IMAGE;
    String extension = mediaType == MediaType.AUDIO ? ".wav" : ".png";
    String contentType = mediaType == MediaType.AUDIO ? "audio/wav" : "image/png";
    String requestedResolution = submission.resolution() != null ? submission.resolution() : "512x512";
    String acceptedResolution = verdict.decision() == AdmissionVerdict.Decision.DEGRADED
        ? degradeResolution(requestedResolution)
        : requestedResolution;
    Map<String, String> metadata = buildSubmissionMetadata(submission, verdict, tier, requestedResolution, acceptedResolution);

    Media media = Media.builder()
        .mediaId(mediaId)
        .tenantId(submission.tenantId())
        .userId(submission.userId())
        .size(0L)
        .name(mediaId + "-generated" + extension)
        .mimetype(contentType)
        .mediaType(mediaType)
        .source(MediaSource.GENERATED)
        .status(MediaStatus.PROCESSING)
        .originalAssetId(assetId)
        .webhookUrl(submission.webhookUrl())
        .createdAt(now)
        .build();

    MediaAsset initialAsset = MediaAsset.builder()
        .assetId(assetId)
        .mediaId(mediaId)
        .tenantId(submission.tenantId())
        .sourceAssetId(null)
        .type(AssetType.ORIGINAL)
        .tags(ORIGINAL_TAGS)
        .status(AssetStatus.PENDING)
        .outputFormat(extension.substring(1))
        .mimetype(contentType)
        .downloadName(mediaId + "-generated" + extension)
        .createdAt(now)
        .build();

    GenerationJob job = GenerationJob.builder()
        .jobId(jobId)
        .mediaId(mediaId)
        .tenantId(submission.tenantId())
        .userId(submission.userId())
        .tier(tier)
        .outputType(submission.outputType())
        .status(GenerationStatus.QUEUED)
        .currentStage(GenerationStage.ADMISSION)
        .prompt(submission.prompt())
        .model(model)
        .resolution(acceptedResolution)
        .seed(submission.seed())
        .webhookUrl(submission.webhookUrl())
        .estimatedWaitSeconds(Math.max(estimateWaitSeconds(submission), verdict.retryAfterSeconds()))
        .aiGenerated(true)
        .metadata(metadata)
        .createdAt(now)
        .updatedAt(now)
        .build();

    return new SubmissionArtifacts(media, initialAsset, job);
  }

  private Map<String, String> buildSubmissionMetadata(GenerationSubmission submission,
      AdmissionVerdict verdict, String tier, String requestedResolution, String acceptedResolution) {
    Map<String, String> metadata = new HashMap<>();
    metadata.put("admission_decision", verdict.decision().name());
    metadata.put("admission_code", verdict.code());
    metadata.put("admission_message", verdict.message());
    metadata.put("retry_after_seconds", String.valueOf(verdict.retryAfterSeconds()));
    metadata.put("requested_resolution", requestedResolution);
    metadata.put("accepted_resolution", acceptedResolution);
    metadata.put("tier", tier);
    metadata.putAll(verdict.metadata());
    if (submission.audioOverviewProvider() != null && !submission.audioOverviewProvider().isBlank()) {
      metadata.put("audio_overview_provider", submission.audioOverviewProvider().toLowerCase(Locale.ROOT));
    }
    return metadata;
  }

  private GenerationJob persistAndPublish(SubmissionArtifacts artifacts, String tier) {
    GenerationJob job = artifacts.job();
    repository.createJob(job, artifacts.media(), artifacts.initialAsset());
    admissionController.recordAccepted(job);
    try {
      publisher.publish(GenerationStageMessage.builder().jobId(job.getJobId()).stage(GenerationStage.ADMISSION)
          .attempt(1).tier(tier).build());
    } catch (RuntimeException e) {
      admissionController.rollback(job);
      repository.failJob(job.getJobId(), "GENERATION_PUBLISH_FAILED", e.getMessage());
      repository.updateMediaStatus(job.getMediaId(), MediaStatus.ERROR);
      repository.updateAssetStatus(job.getMediaId(), artifacts.initialAsset().getAssetId(), AssetStatus.ERROR);
      throw e;
    }
    return job;
  }

  private record SubmissionArtifacts(Media media, MediaAsset initialAsset, GenerationJob job) {
  }

  public Optional<GenerationJob> getJob(String jobId) {
    return repository.getJob(jobId);
  }

  public Optional<ResultView> result(String jobId) {
    return repository.getJob(jobId)
        .filter(job -> job.getStatus() == GenerationStatus.COMPLETE)
        .flatMap(job -> repository.getAsset(job.getMediaId(), job.getResultAssetId())
            .map(asset -> {
              String url = storage.presignedUrl(job.getTenantId(), job.getMediaId(), asset.getAssetId(),
                  job.getResultExtension(), asset.getDownloadName(), asset.getMimetype());
              return new ResultView(job, asset, url, Instant.now().plusSeconds(3600));
            }));
  }

  public void processStage(GenerationStageMessage message) {
    GenerationJob job = repository.getJob(message.getJobId())
        .orElseThrow(() -> new GenerationProviderException(GenerationErrorCode.JOB_NOT_FOUND, "Generation job not found: " + message.getJobId()));
    if (job.getStatus() == GenerationStatus.COMPLETE || job.getStatus() == GenerationStatus.FAILED
        || job.getStatus() == GenerationStatus.BLOCKED) {
      return;
    }
    Instant startedAt = Instant.now();
    try {
      repository.createStageRun(job.getTenantId(), job.getJobId(), message.getStage(), message.getAttempt(),
          GenerationStatus.RUNNING, null);
      switch (message.getStage()) {
        case ADMISSION -> admission(job, message.getAttempt());
        case PREPROCESS -> preprocess(job, message.getAttempt());
        case INFERENCE -> inference(job, message.getAttempt());
        case INFERENCE_POLL -> imageInferencePoll(job, message);
        case POSTPROCESS -> postprocess(job, message.getAttempt());
        case DELIVERY -> delivery(job, message.getAttempt());
      }
      repository.createStageRun(job.getTenantId(), job.getJobId(), message.getStage(), message.getAttempt(),
          GenerationStatus.COMPLETE, null);
      metrics.recordStageLatency(job, message.getStage(), Duration.between(startedAt, Instant.now()), true, null);
    } catch (TransientStageException transientEx) {
      // ExitRedeliver/Reconcile sentinel: let SQS redeliver after visibility timeout.
      // Do NOT advance status, do NOT consume an attempt — surface so the message handler
      // can throw and SQS visibility re-handle.
      log.info("Transient stage exit for job {} stage {}: {}", job.getJobId(), message.getStage(),
          transientEx.getMessage());
      metrics.recordStageLatency(job, message.getStage(), Duration.between(startedAt, Instant.now()), false,
          "TRANSIENT");
      throw transientEx;
    } catch (Exception e) {
      // GenerationProviderException carries an explicit error code; any other unchecked Exception
      // is collapsed to GENERATION_FAILED. Both share the retry-or-fail decision path.
      String code = (e instanceof GenerationProviderException gpe) ? gpe.getCode() : "GENERATION_FAILED";
      log.warn("Stage processing failed: job={} stage={} attempt={} code={} message={}",
          job.getJobId(), message.getStage(), message.getAttempt(), code, e.getMessage(), e);
      repository.createStageRun(job.getTenantId(), job.getJobId(), message.getStage(), message.getAttempt(),
          GenerationStatus.FAILED, code);
      if (shouldRetry(message, code)) {
        repository.releaseStageSideEffectForRetry(job.getJobId(), message.getStage(), code);
        publishRetry(job, message.getStage(), message.getAttempt());
        metrics.recordStageRetry(job, message.getStage(), message.getAttempt() + 1, code);
        metrics.recordStageLatency(job, message.getStage(), Duration.between(startedAt, Instant.now()), false, code);
        return;
      }
      repository.failStageSideEffect(job.getJobId(), message.getStage(), code);
      failAndCleanup(job, code, e.getMessage());
      metrics.recordStageLatency(job, message.getStage(), Duration.between(startedAt, Instant.now()), false, code);
      throw e;
    }
  }

  private void admission(GenerationJob job, int attempt) {
    if (!claimOrReplay(job, GenerationStage.ADMISSION, GenerationStage.PREPROCESS, attempt)) {
      return;
    }
    double estimatedUsd = estimateCostUsd(job);
    double dailyCapUsd = providerFactory.config().dailyBudgetUsd();
    repository.reserveBudget(job.getTenantId(), job.getJobId(), estimatedUsd, dailyCapUsd);
    metrics.recordBudgetUsage(job, estimatedUsd, dailyCapUsd);
    repository.completeStageSideEffect(job.getJobId(), GenerationStage.ADMISSION, "reserved");
    publishNext(job, GenerationStage.PREPROCESS, attempt);
  }

  private void preprocess(GenerationJob job, int attempt) {
    if (!claimOrReplay(job, GenerationStage.PREPROCESS, GenerationStage.INFERENCE, attempt)) {
      return;
    }
    var result = providerFactory.moderationProvider().moderate(job.getTenantId(), job.getPrompt(), "preprocess");
    recordSafety(job, GenerationStage.PREPROCESS, "pre_prompt", result);
    if (!result.allowed()) {
      repository.failStageSideEffect(job.getJobId(), GenerationStage.PREPROCESS, "MODERATION_BLOCKED");
      failAndCleanup(job, "MODERATION_BLOCKED", result.reason());
      return;
    }
    EnhancedPrompt enhanced = providerFactory.promptEnhancer().enhance(job.getTenantId(), job.getPrompt());
    if (enhanced.enhanced()) {
      repository.updateEnhancedPrompt(job.getJobId(), enhanced.prompt());
      job.setEnhancedPrompt(enhanced.prompt());
      var postRewrite = providerFactory.moderationProvider().moderate(job.getTenantId(), enhanced.prompt(), "post_rewrite");
      recordSafety(job, GenerationStage.PREPROCESS, "post_rewrite", postRewrite);
      if (!postRewrite.allowed()) {
        repository.failStageSideEffect(job.getJobId(), GenerationStage.PREPROCESS, "POST_REWRITE_BLOCKED");
        failAndCleanup(job, "POST_REWRITE_BLOCKED", postRewrite.reason());
        return;
      }
    }
    repository.completeStageSideEffect(job.getJobId(), GenerationStage.PREPROCESS, "allowed");
    publishNext(job, GenerationStage.INFERENCE, attempt);
  }

  private void inference(GenerationJob job, int attempt) {
    if (job.getOutputType() == GenerationOutputType.AUDIO) {
      audioInference(job, attempt);
    } else {
      imageInference(job, attempt);
    }
  }

  private void audioInference(GenerationJob job, int attempt) {
    ClaimOutcome outcome = repository.claimStageSideEffectV2(job.getJobId(), GenerationStage.INFERENCE);
    String effectiveProvider = resolveAudioOverviewProvider(job);
    metrics.recordIdempotencyStateTransition(null, outcomeName(outcome), effectiveProvider, GenerationStage.INFERENCE);
    if (outcome instanceof ClaimOutcome.ReuseExisting reuse) {
      log.info("Reusing prior audio overview result {} for job {}", reuse.resultRef(), job.getJobId());
      publishNext(job, GenerationStage.POSTPROCESS, attempt);
      return;
    }
    if (outcome instanceof ClaimOutcome.ExitRedeliver) {
      throw new TransientStageException("Audio overview idempotency row is in-flight; redeliver");
    }
    if (outcome instanceof ClaimOutcome.TerminalFailure failure) {
      throw new GenerationProviderException(failure.errorCode(), failure.errorMessage());
    }
    Artifact artifact = providerFactory.audioOverviewProvider(effectiveProvider).generateOverview(toSpec(job));
    String assetId = persistFinalArtifact(job, artifact);
    double costUsd = estimateCostUsd(job);
    repository.commitBudget(job.getTenantId(), job.getJobId(), costUsd, costUsd);
    metrics.recordBudgetCommitted(job, costUsd);
    metrics.recordArtifactCost(job, GenerationStage.INFERENCE, effectiveProvider, costUsd);
    repository.completeStageSideEffect(job.getJobId(), GenerationStage.INFERENCE, assetId);
    publishNext(job, GenerationStage.POSTPROCESS, attempt);
  }

  private void imageInference(GenerationJob job, int attempt) {
    ImageProvider provider = providerFactory.imageProvider();
    ProviderKind kind = provider.kind();
    GenerationStage nextStage = kind == ProviderKind.ASYNC
        ? GenerationStage.INFERENCE_POLL
        : GenerationStage.POSTPROCESS;
    ClaimOutcome outcome = repository.claimStageSideEffectV2(job.getJobId(), GenerationStage.INFERENCE);
    metrics.recordIdempotencyStateTransition(null, outcomeName(outcome),
        providerFactory.config().provider(), GenerationStage.INFERENCE);
    if (outcome instanceof ClaimOutcome.ReuseExisting reuse) {
      handleInferenceReuse(job, attempt, nextStage, reuse.resultRef());
      return;
    }
    if (outcome instanceof ClaimOutcome.Reconcile) {
      Optional<Artifact> reconciled = provider.reconcile(job.getJobId());
      if (reconciled.isPresent()) {
        persistAndAdvance(job, attempt, GenerationStage.INFERENCE, nextStage, reconciled.get());
        return;
      }
      handleUnrecoverableInference(job, GenerationStage.INFERENCE);
      return;
    }
    if (outcome instanceof ClaimOutcome.ExitRedeliver) {
      throw new TransientStageException("Inference idempotency row is in-flight; redeliver");
    }
    if (outcome instanceof ClaimOutcome.TerminalFailure failure) {
      throw new GenerationProviderException(failure.errorCode(), failure.errorMessage());
    }
    // Proceed: we own the claim. Run the provider call.
    if (kind == ProviderKind.ASYNC) {
      ProviderJobId providerJobId = provider.submitAsync(toSpec(job));
      repository.recordProviderJobId(job.getJobId(), providerJobId.value());
      repository.completeStageSideEffect(job.getJobId(), GenerationStage.INFERENCE, providerJobId.value());
      publishNext(job, GenerationStage.INFERENCE_POLL, attempt);
      return;
    }
    Artifact artifact = provider.generateSync(toSpec(job));
    persistAndAdvance(job, attempt, GenerationStage.INFERENCE, nextStage, artifact);
  }

  /**
   * Persist a successful provider artifact, commit the budget reservation, emit metrics,
   * complete the stage idempotency row, and publish the next stage message.
   */
  private void persistAndAdvance(GenerationJob job, int attempt, GenerationStage currentStage,
      GenerationStage nextStage, Artifact artifact) {
    String assetId = persistFinalArtifact(job, artifact);
    double costUsd = estimateCostUsd(job);
    // Workflow does not yet separate estimate vs actual cost — passing the same value lets
    // the repository decrement pending_usd directly without re-reading the reservation row.
    repository.commitBudget(job.getTenantId(), job.getJobId(), costUsd, costUsd);
    metrics.recordBudgetCommitted(job, costUsd);
    metrics.recordArtifactCost(job, currentStage, providerFactory.config().provider(), costUsd);
    repository.completeStageSideEffect(job.getJobId(), currentStage, assetId);
    publishNext(job, nextStage, attempt);
  }

  private void handleInferenceReuse(GenerationJob job, int attempt, GenerationStage nextStage, String resultRef) {
    // Skip the paid call entirely; just advance the workflow.
    log.info("Reusing prior inference result {} for job {}", resultRef, job.getJobId());
    publishNext(job, nextStage, attempt);
  }

  private void handleUnrecoverableInference(GenerationJob job, GenerationStage stage) {
    repository.failStageSideEffect(job.getJobId(), stage, "UNKNOWN_OUTCOME_UNRECOVERABLE");
    repository.markUnknownOutcomeUnrecoverable(job.getJobId(), stage,
        "UNKNOWN_OUTCOME_UNRECOVERABLE",
        "Reconciliation could not resolve unknown_outcome row for " + stage);
    metrics.recordUnknownOutcomeTerminal(providerFactory.config().provider(), stage);
    throw new GenerationProviderException(GenerationErrorCode.UNKNOWN_OUTCOME_UNRECOVERABLE,
        "Unable to reconcile unknown outcome for stage " + stage);
  }

  private void imageInferencePoll(GenerationJob job, GenerationStageMessage message) {
    int attempt = message.getAttempt();
    int pollCount = message.getPollCount();
    if (repository.getStageSideEffectResult(job.getJobId(), GenerationStage.INFERENCE_POLL).isPresent()) {
      publishNext(job, GenerationStage.POSTPROCESS, attempt);
      return;
    }
    // job was freshly loaded by processStage; providerJobId is written by the prior INFERENCE
    // stage's recordProviderJobId call and not mutated within this method before this point.
    String providerJobId = job.getProviderJobId();
    if (providerJobId == null || providerJobId.isBlank()) {
      throw new GenerationProviderException(GenerationErrorCode.PROVIDER_JOB_MISSING, "Provider job id is missing for async inference");
    }
    ImageProvider provider = providerFactory.imageProvider();
    var state = provider.poll(new ProviderJobId(providerJobId));
    if (state.status() == ProviderStatus.RUNNING) {
      int maxPolls = providerFactory.config().maxPollAttempts();
      if (pollCount + 1 >= maxPolls) {
        throw new GenerationProviderException(GenerationErrorCode.POLL_EXHAUSTED,
            "Async inference poll exhausted after " + maxPolls + " polls");
      }
      publishPollAgain(job, attempt, pollCount + 1);
      return;
    }
    if (state.status() == ProviderStatus.FAILED) {
      throw new GenerationProviderException(GenerationErrorCode.PROVIDER_JOB_FAILED, state.message());
    }
    ClaimOutcome outcome = repository.claimStageSideEffectV2(job.getJobId(), GenerationStage.INFERENCE_POLL);
    metrics.recordIdempotencyStateTransition(null, outcomeName(outcome),
        providerFactory.config().provider(), GenerationStage.INFERENCE_POLL);
    if (outcome instanceof ClaimOutcome.ReuseExisting) {
      publishNext(job, GenerationStage.POSTPROCESS, attempt);
      return;
    }
    if (outcome instanceof ClaimOutcome.ExitRedeliver) {
      throw new TransientStageException("Inference-poll idempotency row is in-flight; redeliver");
    }
    if (outcome instanceof ClaimOutcome.TerminalFailure failure) {
      throw new GenerationProviderException(failure.errorCode(), failure.errorMessage());
    }
    if (outcome instanceof ClaimOutcome.Reconcile) {
      Optional<Artifact> reconciled = provider.reconcile(job.getJobId());
      if (reconciled.isPresent()) {
        persistAndAdvance(job, attempt, GenerationStage.INFERENCE_POLL, GenerationStage.POSTPROCESS,
            reconciled.get());
        return;
      }
      handleUnrecoverableInference(job, GenerationStage.INFERENCE_POLL);
      return;
    }
    Artifact artifact = provider.fetch(new ProviderJobId(providerJobId));
    persistAndAdvance(job, attempt, GenerationStage.INFERENCE_POLL, GenerationStage.POSTPROCESS, artifact);
  }

  private static String outcomeName(ClaimOutcome outcome) {
    if (outcome instanceof ClaimOutcome.Proceed) return "claimed";
    if (outcome instanceof ClaimOutcome.ReuseExisting) return "completed";
    if (outcome instanceof ClaimOutcome.Reconcile) return "unknown_outcome";
    if (outcome instanceof ClaimOutcome.ExitRedeliver) return "in_flight";
    if (outcome instanceof ClaimOutcome.TerminalFailure) return "failed";
    return "unknown";
  }

  private void postprocess(GenerationJob job, int attempt) {
    if (!claimOrReplay(job, GenerationStage.POSTPROCESS, GenerationStage.DELIVERY, attempt)) {
      return;
    }
    log.debug("safety.placeholder=true stage=postprocess job={} (text-only moderation stand-in)", job.getJobId());
    var result = providerFactory.moderationProvider().moderate(job.getTenantId(), "published output for " + job.getPrompt(), "postprocess");
    recordSafety(job, GenerationStage.POSTPROCESS, "output", result);
    if (!result.allowed()) {
      repository.failStageSideEffect(job.getJobId(), GenerationStage.POSTPROCESS, "OUTPUT_BLOCKED");
      failAndCleanup(job, "OUTPUT_BLOCKED", result.reason());
      return;
    }
    // job was freshly loaded by processStage; the inference stage's recordResultArtifact
    // write happened in the prior stage, so resultAssetId fields are already current.
    if (job.getResultAssetId() != null) {
      repository.updateAssetComplete(job.getMediaId(), job.getResultAssetId(),
          job.getResultSizeBytes() != null ? job.getResultSizeBytes() : 0L,
          job.getResultContentType(), job.getResultExtension());
    }
    repository.completeStageSideEffect(job.getJobId(), GenerationStage.POSTPROCESS, "watermark-verified");
    publishNext(job, GenerationStage.DELIVERY, attempt);
  }

  private String resolveAudioOverviewProvider(GenerationJob job) {
    Map<String, String> meta = job.getMetadata();
    if (meta != null) {
      String override = meta.get("audio_overview_provider");
      if (override != null && !override.isBlank()) {
        return override;
      }
    }
    return providerFactory.config().audioOverviewProvider();
  }

  private void delivery(GenerationJob job, int attempt) {
    if (!repository.claimStageSideEffect(job.getJobId(), GenerationStage.DELIVERY)) {
      return;
    }
    if (job.getResultAssetId() != null) {
      long sizeBytes = job.getResultSizeBytes() != null ? job.getResultSizeBytes() : 0L;
      repository.updateAssetComplete(job.getMediaId(), job.getResultAssetId(),
          sizeBytes, job.getResultContentType(), job.getResultExtension());
      repository.updateGeneratedMediaComplete(job.getMediaId(),
          sizeBytes, job.getResultContentType(), job.getResultExtension());
    } else {
      repository.updateMediaStatus(job.getMediaId(), MediaStatus.COMPLETE);
    }
    repository.completeJob(job.getJobId());
    admissionController.release(job);
    repository.getMedia(job.getMediaId()).ifPresent(webhookNotifier::notifyComplete);
    repository.completeStageSideEffect(job.getJobId(), GenerationStage.DELIVERY, "delivered");
  }

  private String persistFinalArtifact(GenerationJob job, Artifact artifact) {
    // job is the freshly-loaded copy from processStage; the fields read here (jobId, mediaId,
    // tenantId, outputType) are immutable post-submission, so an extra getJob() round-trip is
    // pure overhead.
    if (job.getOutputType() == GenerationOutputType.IMAGE && ImageWatermarker.needsStamp(artifact)) {
      artifact = ImageWatermarker.stamp(artifact);
    }
    verifyPublishableArtifact(job, artifact);
    String assetId = repository.getMedia(job.getMediaId()).map(Media::getOriginalAssetId)
        .orElseThrow(() -> new GenerationProviderException(GenerationErrorCode.MEDIA_NOT_FOUND, "Media row missing for job " + job.getJobId()));
    storage.put(job.getTenantId(), job.getMediaId(), assetId, artifact);
    repository.createArtifact(GenerationArtifact.builder()
        .artifactId(UUID.randomUUID().toString())
        .jobId(job.getJobId())
        .mediaId(job.getMediaId())
        .assetId(assetId)
        .tenantId(job.getTenantId())
        .artifactType(job.getOutputType().name().toLowerCase())
        .uri(StorageConstants.buildAssetKey(job.getTenantId(), job.getMediaId(), assetId, artifact.extension()))
        .contentType(artifact.contentType())
        .extension(artifact.extension())
        .sizeBytes((long) artifact.bytes().length)
        .checksum(checksum(artifact.bytes()))
        .metadata(artifact.metadata())
        .createdAt(Instant.now())
        .build());
    repository.recordResultArtifact(job.getJobId(), assetId, artifact.contentType(), artifact.extension(), artifact.bytes().length);
    return assetId;
  }

  private void verifyPublishableArtifact(GenerationJob job, Artifact artifact) {
    Map<String, String> metadata = artifact.metadata() != null ? artifact.metadata() : Map.of();
    switch (job.getOutputType()) {
      case IMAGE -> verifyImageArtifact(job, artifact, metadata);
      case AUDIO -> verifyAudioArtifact(artifact, metadata);
    }
  }

  private void verifyImageArtifact(GenerationJob job, Artifact artifact, Map<String, String> metadata) {
    boolean watermarkPresent = metadata.containsKey("watermark");
    metrics.recordWatermarkVerification(job, watermarkPresent);
    if (!watermarkPresent) {
      throw new GenerationProviderException(GenerationErrorCode.WATERMARK_MISSING,
          "Generated image artifact is missing verified AI watermark metadata");
    }
    if (!metadata.containsKey("content_safety")) {
      throw new GenerationProviderException(GenerationErrorCode.OUTPUT_SAFETY_MISSING,
          "Generated image artifact is missing output safety metadata");
    }
    if (!looksLikePng(artifact.bytes())) {
      throw new GenerationProviderException(GenerationErrorCode.OUTPUT_SAFETY_MISSING,
          "Generated image artifact failed simulator byte-level output safety verification");
    }
  }

  private void verifyAudioArtifact(Artifact artifact, Map<String, String> metadata) {
    if (!"true".equals(metadata.get("is_ai_generated")) || metadata.getOrDefault("disclosure", "").isBlank()) {
      throw new GenerationProviderException(GenerationErrorCode.AI_DISCLOSURE_MISSING,
          "Generated audio artifact is missing AI-generated disclosure metadata");
    }
    if (requiresEmbeddedAudioDisclosure(artifact)
        && !NotebookLmAudioOverviewProvider.containsDisclosureMarker(artifact.bytes())) {
      throw new GenerationProviderException(GenerationErrorCode.AI_DISCLOSURE_MISSING,
          "Generated audio artifact is missing embedded AI-generated audio disclosure marker");
    }
  }

  private boolean requiresEmbeddedAudioDisclosure(Artifact artifact) {
    String contentType = artifact.contentType() != null ? artifact.contentType().toLowerCase() : "";
    String extension = artifact.extension() != null ? artifact.extension().toLowerCase() : "";
    // NotebookLM currently returns an ISO BMFF/M4A payload. Mutating that container
    // with an ID3 prefix breaks browser playback, so its publish gate is metadata-only.
    return !contentType.contains("mp4") && !extension.equals(".m4a");
  }

  private boolean looksLikePng(byte[] bytes) {
    return bytes != null && bytes.length >= 8
        && (bytes[0] & 0xff) == 0x89
        && bytes[1] == 'P'
        && bytes[2] == 'N'
        && bytes[3] == 'G';
  }

  private JobSpec toSpec(GenerationJob job) {
    String prompt = job.getEnhancedPrompt() != null && !job.getEnhancedPrompt().isBlank()
        ? job.getEnhancedPrompt()
        : job.getPrompt();
    return new JobSpec(job.getJobId(), job.getMediaId(), job.getTenantId(), job.getOutputType(), prompt,
        job.getModel(), job.getResolution(), job.getSeed(),
        Map.of("aiGenerated", "true", "region", providerFactory.config().region(), "tier", normalizeTier(job.getTier())));
  }

  private void recordSafety(GenerationJob job, GenerationStage stage, String gate,
      com.mediaservice.common.generation.provider.ModerationResult result) {
    repository.createSafetyDecision(job.getTenantId(), job.getJobId(), stage, gate, result);
    metrics.recordSafetyDecision(job, stage, gate, result.allowed(), result.reason(), result.classifier());
  }

  private void publishNext(GenerationJob job, GenerationStage nextStage, int attempt) {
    publishStage(job, nextStage, attempt, 0);
  }

  private void publishRetry(GenerationJob job, GenerationStage stage, int attempt) {
    publishStage(job, stage, attempt + 1, 0);
  }

  private void publishPollAgain(GenerationJob job, int attempt, int pollCount) {
    // Polls increment pollCount, NOT attempt — the stage retry budget protects against
    // sporadic provider errors; the poll budget protects against runaway long-running async jobs.
    publishStage(job, GenerationStage.INFERENCE_POLL, attempt, pollCount);
  }

  private void publishStage(GenerationJob job, GenerationStage stage, int attempt, int pollCount) {
    repository.updateJobStage(job.getJobId(), GenerationStatus.RUNNING, stage);
    publisher.publish(GenerationStageMessage.builder()
        .jobId(job.getJobId())
        .stage(stage)
        .attempt(attempt)
        .pollCount(pollCount)
        .tier(normalizeTier(job.getTier()))
        .build());
  }

  private boolean shouldRetry(GenerationStageMessage message, String errorCode) {
    if (message.getAttempt() >= providerFactory.config().maxStageAttempts()) {
      return false;
    }
    return switch (errorCode) {
      case "BUDGET_EXCEEDED", "MODERATION_BLOCKED", "POST_REWRITE_BLOCKED", "OUTPUT_BLOCKED", "AI_DISCLOSURE_MISSING",
          "WATERMARK_MISSING", "OUTPUT_SAFETY_MISSING", "NOT_CONFIGURED", "UNSUPPORTED_PROVIDER",
          "POLL_EXHAUSTED", "UNKNOWN_OUTCOME_UNRECOVERABLE",
          "NOTEBOOKLM_AUTH_EXPIRED", "NOTEBOOKLM_AUTH_MISSING", "NOTEBOOKLM_NOT_CONFIGURED" -> false;
      default -> true;
    };
  }

  private boolean claimOrReplay(GenerationJob job, GenerationStage stage, GenerationStage nextStage, int attempt) {
    ClaimOutcome outcome = repository.claimStageSideEffectV2(job.getJobId(), stage);
    metrics.recordIdempotencyStateTransition(null, outcomeName(outcome), providerFactory.config().provider(), stage);
    if (outcome instanceof ClaimOutcome.Proceed) {
      return true;
    }
    if (outcome instanceof ClaimOutcome.ReuseExisting) {
      publishNext(job, nextStage, attempt);
      return false;
    }
    if (outcome instanceof ClaimOutcome.TerminalFailure failure) {
      throw new GenerationProviderException(failure.errorCode(), failure.errorMessage());
    }
    throw new TransientStageException("Stage idempotency row is in-flight or unresolved for " + stage);
  }

  private double estimateCostUsd(GenerationJob job) {
    if (job.getOutputType() == GenerationOutputType.AUDIO) {
      return roundCost(0.012);
    }
    double megapixels = 0.262144;
    if (job.getResolution() != null && job.getResolution().contains("x")) {
      try {
        String[] parts = job.getResolution().toLowerCase().split("x");
        megapixels = (Integer.parseInt(parts[0].trim()) * Integer.parseInt(parts[1].trim())) / 1_000_000.0;
      } catch (NumberFormatException ignored) {
        megapixels = 0.262144;
      }
    }
    return roundCost(0.01 + megapixels * 0.02);
  }

  private int estimateWaitSeconds(GenerationSubmission submission) {
    long millis = providerFactory.config().simulatorColdStartMs() + providerFactory.config().simulatorMeanDurationMs();
    return Math.max(1, (int) Math.ceil(millis / 1000.0));
  }

  private double roundCost(double value) {
    return Math.round(value * 1_000_000.0) / 1_000_000.0;
  }

  private String degradeResolution(String resolution) {
    if (resolution == null || !resolution.contains("x")) {
      return "256x256";
    }
    try {
      String[] parts = resolution.toLowerCase().split("x");
      int width = Math.max(128, Integer.parseInt(parts[0].trim()) / 2);
      int height = Math.max(128, Integer.parseInt(parts[1].trim()) / 2);
      return width + "x" + height;
    } catch (NumberFormatException e) {
      return "256x256";
    }
  }

  private String normalizeTier(String tier) {
    // Convert at the boundary — API DTOs and DynamoDB storage stay lowercase strings.
    return Tier.fromString(tier).wireValue();
  }

  private void failAndCleanup(GenerationJob job, String errorCode, String errorMessage) {
    repository.failJob(job.getJobId(), errorCode, errorMessage);
    repository.updateMediaStatus(job.getMediaId(), MediaStatus.ERROR);
    failPlaceholderAsset(job);
    releaseReservedBudget(job);
    admissionController.release(job);
  }

  private void failPlaceholderAsset(GenerationJob job) {
    // Without this the placeholder ORIGINAL asset created at submit() stays AssetStatus.PENDING
    // forever, so the UI gallery card and Media Lab header disagree with the parent media row.
    try {
      repository.getMedia(job.getMediaId())
          .map(Media::getOriginalAssetId)
          .filter(id -> id != null && !id.isBlank())
          .ifPresent(assetId -> repository.updateAssetStatus(job.getMediaId(), assetId, AssetStatus.ERROR));
    } catch (RuntimeException e) {
      log.warn("Failed to flip placeholder asset to ERROR for media {}: {}", job.getMediaId(), e.getMessage());
    }
  }

  private void releaseReservedBudget(GenerationJob job) {
    try {
      repository.releaseBudget(job.getTenantId(), job.getJobId());
    } catch (RuntimeException e) {
      log.warn("Failed to release reserved budget for generation job {}", job.getJobId(), e);
    }
  }

  private String prefixedId(String prefix) {
    return prefix + "_" + UUID.randomUUID().toString().replace("-", "");
  }

  private String checksum(byte[] bytes) {
    try {
      return HexFormat.of().formatHex(MessageDigest.getInstance("SHA-256").digest(bytes));
    } catch (Exception e) {
      throw new GenerationProviderException(GenerationErrorCode.CHECKSUM_FAILED, e.getMessage());
    }
  }

  public record ResultView(GenerationJob job, MediaAsset asset, String url, Instant expiresAt) {
  }
}
