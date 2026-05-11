package com.mediaservice.providers.generation.core;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.mediaservice.common.generation.GenerationArtifact;
import com.mediaservice.common.generation.GenerationJob;
import com.mediaservice.common.generation.GenerationOutputType;
import com.mediaservice.common.generation.GenerationStage;
import com.mediaservice.common.generation.GenerationStageMessage;
import com.mediaservice.common.generation.GenerationStatus;
import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.providers.generation.image.ImageProvider;
import com.mediaservice.common.generation.provider.JobSpec;
import com.mediaservice.common.generation.provider.ModerationResult;
import com.mediaservice.common.generation.provider.ProviderJobId;
import com.mediaservice.common.generation.provider.ProviderKind;
import com.mediaservice.common.generation.provider.ProviderState;
import com.mediaservice.common.generation.provider.ProviderStatus;
import com.mediaservice.common.model.AssetStatus;
import com.mediaservice.common.model.Media;
import com.mediaservice.common.model.MediaAsset;
import com.mediaservice.common.model.MediaStatus;
import java.time.Duration;
import java.time.Instant;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.Set;
import org.junit.jupiter.api.Test;
import com.mediaservice.providers.generation.image.OpenAIImageProvider;
import com.mediaservice.providers.generation.shared.OpenAIClient;

class GenerationWorkflowSimulatorTest {
  @Test
  void simulatorImageGenerationRunsEndToEnd() {
    var repository = new MemoryRepository();
    var storage = new MemoryStorage();
    var publisher = new QueuePublisher();
    var webhook = new CountingWebhook();
    var factory = new GenerationProviderFactory(GenerationRuntimeConfig.simulatorDefaults(), null, "media");
    var workflow = new GenerationWorkflow(repository, factory, storage, publisher, webhook);

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.IMAGE, "a simulated mountain", null,
        "256x256", 7L, "https://example.com/webhook"));

    drain(workflow, publisher);

    GenerationJob complete = workflow.getJob(submitted.getJobId()).orElseThrow();
    assertThat(complete.getStatus()).isEqualTo(GenerationStatus.COMPLETE);
    assertThat(complete.getMediaId()).isEqualTo(submitted.getMediaId());
    assertThat(repository.media.get(complete.getMediaId()).getStatus()).isEqualTo(MediaStatus.COMPLETE);
    assertThat(repository.assets.get(complete.getMediaId()).getStatus()).isEqualTo(AssetStatus.COMPLETE);
    assertThat(storage.putCount).isEqualTo(1);
    assertThat(storage.bytesByAsset.get(complete.getResultAssetId())).isNotEmpty();
    assertThat(repository.stageRuns).contains(GenerationStage.ADMISSION, GenerationStage.PREPROCESS,
        GenerationStage.INFERENCE, GenerationStage.POSTPROCESS, GenerationStage.DELIVERY);
    assertThat(repository.safetyDecisions).hasSize(3);
    assertThat(repository.budgetReserved).isTrue();
    assertThat(repository.budgetCommitted).isTrue();
    assertThat(webhook.count).isEqualTo(1);
    assertThat(workflow.result(complete.getJobId()).orElseThrow().url()).startsWith("https://local.test/");

    workflow.processStage(GenerationStageMessage.builder()
        .jobId(complete.getJobId())
        .stage(GenerationStage.INFERENCE)
        .attempt(2)
        .build());
    assertThat(storage.putCount).isEqualTo(1);
  }

  @Test
  void simulatorAudioOverviewCompletesInOneInferenceStage() {
    var repository = new MemoryRepository();
    var storage = new MemoryStorage();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new GenerationProviderFactory(GenerationRuntimeConfig.simulatorDefaults(), null, "media"),
        storage, publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.AUDIO, "database internals", null,
        null, null, null));

    drain(workflow, publisher);

    GenerationJob complete = workflow.getJob(submitted.getJobId()).orElseThrow();
    assertThat(complete.getStatus()).isEqualTo(GenerationStatus.COMPLETE);
    assertThat(complete.getOutputType()).isEqualTo(GenerationOutputType.AUDIO);
    assertThat(complete.getResultContentType()).isEqualTo("audio/wav");
    assertThat(repository.stageRuns)
        .containsExactly(GenerationStage.ADMISSION, GenerationStage.ADMISSION,
            GenerationStage.PREPROCESS, GenerationStage.PREPROCESS,
            GenerationStage.INFERENCE, GenerationStage.INFERENCE,
            GenerationStage.POSTPROCESS, GenerationStage.POSTPROCESS,
            GenerationStage.DELIVERY, GenerationStage.DELIVERY);
    assertThat(repository.artifacts.stream()
        .filter(a -> "audio".equals(a.getArtifactType()))
        .findFirst()
        .orElseThrow()
        .getMetadata())
        .containsEntry("is_ai_generated", "true")
        .containsKey("disclosure");
    assertThat(storage.bytesByAsset.get(complete.getResultAssetId())).isNotEmpty();
    assertThat(repository.assets.get(complete.getMediaId()).getStatus()).isEqualTo(AssetStatus.COMPLETE);
  }

  @Test
  void admissionBackpressureRejectsBeforeCreatingJob() {
    var repository = new MemoryRepository();
    var publisher = new QueuePublisher();
    var metrics = new RecordingMetrics();
    var workflow = new GenerationWorkflow(repository,
        new GenerationProviderFactory(GenerationRuntimeConfig.simulatorDefaults(), null, "media"),
        new MemoryStorage(), publisher, new CountingWebhook(),
        submission -> AdmissionVerdict.reject("ADMISSION_BACKPRESSURE", "queue saturated", 30),
        metrics);

    assertThatThrownBy(() -> workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.IMAGE, "a simulated mountain", null,
        "256x256", null, null)))
        .isInstanceOf(GenerationProviderException.class)
        .hasMessageContaining("queue saturated");

    assertThat(repository.jobs).isEmpty();
    assertThat(publisher.messages).isEmpty();
    assertThat(metrics.admissionRejected).isEqualTo(1);
  }

  @Test
  void degradedAdmissionLowersResolutionAndReportsVerdict() {
    var repository = new MemoryRepository();
    var workflow = new GenerationWorkflow(repository,
        new GenerationProviderFactory(GenerationRuntimeConfig.simulatorDefaults(), null, "media"),
        new MemoryStorage(), new QueuePublisher(), new CountingWebhook(),
        submission -> AdmissionVerdict.degraded("ADMISSION_DEGRADED", "queue pressure", 45,
            Map.of("queue_depth", "80")),
        GenerationMetrics.noop());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.IMAGE, "a simulated mountain", null,
        "512x512", null, null));

    assertThat(submitted.getResolution()).isEqualTo("256x256");
    assertThat(submitted.getEstimatedWaitSeconds()).isGreaterThanOrEqualTo(45);
    assertThat(submitted.getMetadata())
        .containsEntry("admission_decision", "DEGRADED")
        .containsEntry("admission_code", "ADMISSION_DEGRADED")
        .containsEntry("requested_resolution", "512x512")
        .containsEntry("accepted_resolution", "256x256");
  }

  @Test
  void budgetCapFailureMarksJobAndMediaErrorAndReleasesReservation() {
    var repository = new MemoryRepository();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new GenerationProviderFactory(configWithDailyBudget(0.001), null, "media"),
        new MemoryStorage(), publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.IMAGE, "a simulated mountain", null,
        "256x256", null, null));

    assertThatThrownBy(() -> drain(workflow, publisher))
        .isInstanceOf(GenerationProviderException.class)
        .hasMessageContaining("budget");

    GenerationJob failed = workflow.getJob(submitted.getJobId()).orElseThrow();
    assertThat(failed.getStatus()).isEqualTo(GenerationStatus.FAILED);
    assertThat(failed.getErrorCode()).isEqualTo("BUDGET_EXCEEDED");
    assertThat(repository.media.get(failed.getMediaId()).getStatus()).isEqualTo(MediaStatus.ERROR);
    assertThat(repository.budgetReleased).isTrue();
  }

  @Test
  void simulatorModerationBlocksUnsafePrompt() {
    var repository = new MemoryRepository();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new GenerationProviderFactory(GenerationRuntimeConfig.simulatorDefaults(), null, "media"),
        new MemoryStorage(), publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.IMAGE, "unsafe prompt", null,
        "256x256", null, null));

    drain(workflow, publisher);

    GenerationJob failed = workflow.getJob(submitted.getJobId()).orElseThrow();
    assertThat(failed.getStatus()).isEqualTo(GenerationStatus.FAILED);
    assertThat(failed.getErrorCode()).isEqualTo("MODERATION_BLOCKED");
    assertThat(repository.safetyDecisions).hasSize(1);
  }

  @Test
  void postRewriteModerationBlocksEnhancedUnsafePrompt() {
    var repository = new MemoryRepository();
    var storage = new MemoryStorage();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new GenerationProviderFactory(GenerationRuntimeConfig.simulatorDefaults(), null, "media"),
        storage, publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.IMAGE, "rewrite-for-denied-output landscape", null,
        "256x256", null, null));

    drain(workflow, publisher);

    GenerationJob failed = workflow.getJob(submitted.getJobId()).orElseThrow();
    assertThat(failed.getStatus()).isEqualTo(GenerationStatus.FAILED);
    assertThat(failed.getErrorCode()).isEqualTo("POST_REWRITE_BLOCKED");
    assertThat(repository.safetyDecisions).hasSize(2);
    assertThat(repository.budgetReleased).isTrue();
    assertThat(storage.putCount).isEqualTo(0);
  }

  @Test
  void audioOverviewCompletionUsesActualArtifactFormatForMediaAndAsset() {
    var repository = new MemoryRepository();
    var storage = new MemoryStorage();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new Mp3AudioOverviewFactory(GenerationRuntimeConfig.simulatorDefaults()),
        storage, publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.AUDIO, "notebook style mp3", null,
        null, null, null));

    drain(workflow, publisher);

    GenerationJob complete = workflow.getJob(submitted.getJobId()).orElseThrow();
    Media media = repository.media.get(complete.getMediaId());
    MediaAsset asset = repository.assets.get(complete.getMediaId());

    assertThat(complete.getStatus()).isEqualTo(GenerationStatus.COMPLETE);
    assertThat(media.getMimetype()).isEqualTo("audio/mpeg");
    assertThat(media.getName()).endsWith(".mp3");
    assertThat(asset.getMimetype()).isEqualTo("audio/mpeg");
    assertThat(asset.getOutputFormat()).isEqualTo("mp3");
    assertThat(asset.getDownloadName()).endsWith(".mp3");
  }

  @Test
  void audioOverviewAcceptsM4aWithMetadataDisclosure() {
    var repository = new MemoryRepository();
    var storage = new MemoryStorage();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new M4aAudioOverviewFactory(GenerationRuntimeConfig.simulatorDefaults()),
        storage, publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.AUDIO, "notebook style m4a", null,
        null, null, null));

    drain(workflow, publisher);

    GenerationJob complete = workflow.getJob(submitted.getJobId()).orElseThrow();
    Media media = repository.media.get(complete.getMediaId());
    MediaAsset asset = repository.assets.get(complete.getMediaId());

    assertThat(complete.getStatus()).isEqualTo(GenerationStatus.COMPLETE);
    assertThat(media.getMimetype()).isEqualTo("audio/mp4");
    assertThat(media.getName()).endsWith(".m4a");
    assertThat(asset.getMimetype()).isEqualTo("audio/mp4");
    assertThat(asset.getOutputFormat()).isEqualTo("m4a");
    assertThat(asset.getDownloadName()).endsWith(".m4a");
    assertThat(storage.putCount).isEqualTo(1);
  }

  @Test
  void missingAudioDisclosureFailsBeforeDelivery() {
    var repository = new MemoryRepository();
    var storage = new MemoryStorage();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new BadAudioDisclosureFactory(GenerationRuntimeConfig.simulatorDefaults()),
        storage, publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.AUDIO, "database internals", null,
        null, null, null));

    assertThatThrownBy(() -> drain(workflow, publisher))
        .isInstanceOf(GenerationProviderException.class)
        .hasMessageContaining("disclosure");

    GenerationJob failed = workflow.getJob(submitted.getJobId()).orElseThrow();
    assertThat(failed.getStatus()).isEqualTo(GenerationStatus.FAILED);
    assertThat(failed.getErrorCode()).isEqualTo("AI_DISCLOSURE_MISSING");
    assertThat(storage.putCount).isEqualTo(0);
  }

  @Test
  void missingImageSafetyMetadataFailsBeforeDelivery() {
    var repository = new MemoryRepository();
    var storage = new MemoryStorage();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new BadImageSafetyFactory(GenerationRuntimeConfig.simulatorDefaults()),
        storage, publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.IMAGE, "database internals", null,
        "256x256", null, null));

    assertThatThrownBy(() -> drain(workflow, publisher))
        .isInstanceOf(GenerationProviderException.class)
        .hasMessageContaining("safety");

    GenerationJob failed = workflow.getJob(submitted.getJobId()).orElseThrow();
    assertThat(failed.getStatus()).isEqualTo(GenerationStatus.FAILED);
    assertThat(failed.getErrorCode()).isEqualTo("OUTPUT_SAFETY_MISSING");
    assertThat(storage.putCount).isEqualTo(0);
  }

  @Test
  void unclaimedStageWithoutCompletedResultRedeliversInsteadOfCompleting() {
    var repository = new InFlightPreprocessRepository();
    var storage = new MemoryStorage();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new GenerationProviderFactory(GenerationRuntimeConfig.simulatorDefaults(), null, "media"),
        storage, publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.IMAGE, "in flight preprocess", null,
        "256x256", null, null));

    workflow.processStage(publisher.messages.removeFirst());
    GenerationStageMessage preprocess = publisher.messages.removeFirst();
    assertThat(preprocess.getStage()).isEqualTo(GenerationStage.PREPROCESS);

    assertThatThrownBy(() -> workflow.processStage(preprocess))
        .isInstanceOf(TransientStageException.class)
        .hasMessageContaining("PREPROCESS");

    GenerationJob job = workflow.getJob(submitted.getJobId()).orElseThrow();
    assertThat(job.getStatus()).isEqualTo(GenerationStatus.RUNNING);
    assertThat(job.getCurrentStage()).isEqualTo(GenerationStage.PREPROCESS);
    assertThat(repository.getStageSideEffectResult(job.getJobId(), GenerationStage.PREPROCESS)).isEmpty();
    assertThat(publisher.messages).isEmpty();
    assertThat(storage.putCount).isZero();
  }

  @Test
  void realProviderStubsFailNotConfiguredWithoutSecrets() {
    var config = GenerationRuntimeConfig.simulatorDefaults();
    assertThatThrownBy(() -> new OpenAIImageProvider(config, new OpenAIClient(config)).generateSync(null))
        .isInstanceOf(NotConfiguredException.class)
        .hasMessageContaining("NOT_CONFIGURED")
        .hasMessageContaining("GENERATION_OPENAI_API_KEY");
  }

  @Test
  void unknownOutcomeReconciliationSucceedsRecoversArtifact() {
    var repository = new UnknownOutcomeRepository(true);
    ActiveReconcileCounter.active = repository;
    try {
      var storage = new MemoryStorage();
      var publisher = new QueuePublisher();
      var factory = new ReconcilableImageFactory(GenerationRuntimeConfig.simulatorDefaults(), true);
      var workflow = new GenerationWorkflow(repository, factory, storage, publisher, new CountingWebhook());

      GenerationJob submitted = workflow.submit(new GenerationSubmission(
          "tenant-a", "user-a", GenerationOutputType.IMAGE, "reconciliation success", null,
          "256x256", null, null));

      drain(workflow, publisher);

      GenerationJob complete = workflow.getJob(submitted.getJobId()).orElseThrow();
      assertThat(complete.getStatus()).isEqualTo(GenerationStatus.COMPLETE);
      assertThat(storage.putCount).isEqualTo(1);
      assertThat(repository.reconcileCalls).isGreaterThanOrEqualTo(1);
    } finally {
      ActiveReconcileCounter.active = null;
    }
  }

  @Test
  void unknownOutcomeReconciliationFailureTerminatesWithUnrecoverable() {
    var repository = new UnknownOutcomeRepository(true);
    ActiveReconcileCounter.active = repository;
    try {
      var storage = new MemoryStorage();
      var publisher = new QueuePublisher();
      var factory = new ReconcilableImageFactory(GenerationRuntimeConfig.simulatorDefaults(), false);
      var workflow = new GenerationWorkflow(repository, factory, storage, publisher, new CountingWebhook());

      GenerationJob submitted = workflow.submit(new GenerationSubmission(
          "tenant-a", "user-a", GenerationOutputType.IMAGE, "reconciliation fail", null,
          "256x256", null, null));

      assertThatThrownBy(() -> drain(workflow, publisher))
          .isInstanceOfSatisfying(GenerationProviderException.class,
              ex -> assertThat(ex.getCode()).isEqualTo("UNKNOWN_OUTCOME_UNRECOVERABLE"));

      GenerationJob failed = workflow.getJob(submitted.getJobId()).orElseThrow();
      assertThat(failed.getStatus()).isEqualTo(GenerationStatus.FAILED);
      assertThat(failed.getErrorCode()).isEqualTo("UNKNOWN_OUTCOME_UNRECOVERABLE");
      assertThat(storage.putCount).isEqualTo(0);
    } finally {
      ActiveReconcileCounter.active = null;
    }
  }

  @Test
  void concurrentReservationsAtNinetyFivePercentCapAdmitExactlyOne() {
    var repository = new AggregateBudgetRepository(1.00);
    repository.reserveBudget("tenant-a", "job-a", 0.95, 1.00);
    assertThatThrownBy(() -> repository.reserveBudget("tenant-a", "job-b", 0.95, 1.00))
        .isInstanceOf(GenerationProviderException.class)
        .hasMessageContaining("budget");
    assertThat(repository.pendingUsd).isEqualTo(0.95);
  }

  @Test
  void reserveCommitReleaseAreIdempotent() {
    var repository = new AggregateBudgetRepository(50.0);
    repository.reserveBudget("tenant-a", "job-1", 1.0, 50.0);
    repository.commitBudget("tenant-a", "job-1", 1.0, 0.9);
    repository.releaseBudget("tenant-a", "job-1");
    repository.releaseBudget("tenant-a", "job-1");
    assertThat(repository.committedUsd).isEqualTo(0.9);
    assertThat(repository.pendingUsd).isEqualTo(0.0);

    repository.reserveBudget("tenant-a", "job-2", 0.5, 50.0);
    repository.releaseBudget("tenant-a", "job-2");
    repository.releaseBudget("tenant-a", "job-2");
    assertThat(repository.pendingUsd).isEqualTo(0.0);
  }

  @Test
  void pollExhaustedFailsStageAfterExceedingCap() {
    GenerationRuntimeConfig config = configWithPollCap(GenerationRuntimeConfig.simulatorDefaults(), 2);
    var repository = new MemoryRepository();
    var storage = new MemoryStorage();
    var publisher = new QueuePublisher();
    var workflow = new GenerationWorkflow(repository,
        new AlwaysRunningAsyncFactory(config), storage, publisher, new CountingWebhook());

    GenerationJob submitted = workflow.submit(new GenerationSubmission(
        "tenant-a", "user-a", GenerationOutputType.IMAGE, "always running", null,
        "256x256", null, null));

    assertThatThrownBy(() -> drain(workflow, publisher))
        .isInstanceOfSatisfying(GenerationProviderException.class,
            ex -> assertThat(ex.getCode()).isEqualTo("POLL_EXHAUSTED"));
    GenerationJob failed = workflow.getJob(submitted.getJobId()).orElseThrow();
    assertThat(failed.getStatus()).isEqualTo(GenerationStatus.FAILED);
    assertThat(failed.getErrorCode()).isEqualTo("POLL_EXHAUSTED");
  }

  @Test
  void failedIdempotencyRowIsNotReclaimed() {
    var repository = new FailedRowRepository();
    repository.failedStages.add("job-x:" + GenerationStage.INFERENCE);
    var outcome = repository.claimStageSideEffectV2("job-x", GenerationStage.INFERENCE);
    assertThat(outcome).isInstanceOf(ClaimOutcome.TerminalFailure.class);
  }

  @Test
  void openAiClientRetriesOnRateLimitThenSucceeds() throws Exception {
    GenerationRuntimeConfig config = configWithOpenAiKey(GenerationRuntimeConfig.simulatorDefaults(),
        "test-key", 3);
    var http = new FakeHttpClient();
    http.responses.add(new FakeHttpClient.Reply(429, "{\"error\":{\"message\":\"slow down\"}}"));
    http.responses.add(new FakeHttpClient.Reply(200, "{\"ok\":true}"));

    var client = new OpenAIClient(config, http);
    var body = new com.fasterxml.jackson.databind.ObjectMapper().createObjectNode().put("k", "v");
    var response = client.postJson("/test", body, "req-1");

    assertThat(response.get("ok").asBoolean()).isTrue();
    assertThat(http.calls).isEqualTo(2);
    assertThat(http.lastRequest.headers().firstValue("Idempotency-Key")).hasValue("req-1");
  }

  @Test
  void openAiClient401ForcesSecretRefreshAndOneRetry() throws Exception {
    GenerationRuntimeConfig config = configWithOpenAiKey(GenerationRuntimeConfig.simulatorDefaults(),
        "stale-key", 3);
    var http = new FakeHttpClient();
    http.responses.add(new FakeHttpClient.Reply(401, "{\"error\":{\"message\":\"invalid_api_key\"}}"));
    http.responses.add(new FakeHttpClient.Reply(200, "{\"ok\":true}"));

    var client = new OpenAIClient(config, http);
    var body = new com.fasterxml.jackson.databind.ObjectMapper().createObjectNode().put("k", "v");
    var response = client.postJson("/test", body, "req-2");

    assertThat(response.get("ok").asBoolean()).isTrue();
    assertThat(http.calls).isEqualTo(2);
  }

  @Test
  void openAiClient4xxNonRetryableSurfacesImmediately() throws Exception {
    GenerationRuntimeConfig config = configWithOpenAiKey(GenerationRuntimeConfig.simulatorDefaults(),
        "test-key", 3);
    var http = new FakeHttpClient();
    http.responses.add(new FakeHttpClient.Reply(400, "{\"error\":{\"message\":\"bad request\"}}"));

    var client = new OpenAIClient(config, http);
    var body = new com.fasterxml.jackson.databind.ObjectMapper().createObjectNode().put("k", "v");
    assertThatThrownBy(() -> client.postJson("/test", body, "req-3"))
        .isInstanceOfSatisfying(GenerationProviderException.class,
            ex -> assertThat(ex.getCode()).isEqualTo("OPENAI_CLIENT_ERROR"));
    assertThat(http.calls).isEqualTo(1);
  }

  private static void drain(GenerationWorkflow workflow, QueuePublisher publisher) {
    while (!publisher.messages.isEmpty()) {
      workflow.processStage(publisher.messages.removeFirst());
    }
  }

  private static GenerationRuntimeConfig configWithOpenAiKey(GenerationRuntimeConfig defaults, String apiKey,
      int maxProviderAttempts) {
    return new GenerationRuntimeConfig(
        defaults.provider(),
        defaults.moderationProvider(),
        defaults.audioOverviewProvider(),
        defaults.llmProvider(),
        defaults.region(),
        defaults.model(),
        defaults.llmModel(),
        defaults.simulatorKind(),
        defaults.simulatorMeanDurationMs(),
        defaults.simulatorColdStartMs(),
        defaults.simulatorFailureRate(),
        defaults.simulatorChaosBusinessHoursEnabled(),
        defaults.simulatorChaosFailureRate(),
        defaults.simulatorChaosStartHourUtc(),
        defaults.simulatorChaosEndHourUtc(),
        defaults.dailyBudgetUsd(),
        defaults.budgetAlertPct(),
        defaults.providerTimeout(),
        apiKey,
        defaults.openAiApiKeySecretArn(),
        defaults.promptEnhancementEnabled(),
        defaults.maxStageAttempts(),
        maxProviderAttempts,
        defaults.secretCacheTtlMillis(),
        defaults.maxPollAttempts());
  }

  private static GenerationRuntimeConfig configWithPollCap(GenerationRuntimeConfig defaults, int maxPollAttempts) {
    return new GenerationRuntimeConfig(
        defaults.provider(),
        defaults.moderationProvider(),
        defaults.audioOverviewProvider(),
        defaults.llmProvider(),
        defaults.region(),
        defaults.model(),
        defaults.llmModel(),
        ProviderKind.ASYNC,
        defaults.simulatorMeanDurationMs(),
        defaults.simulatorColdStartMs(),
        defaults.simulatorFailureRate(),
        defaults.simulatorChaosBusinessHoursEnabled(),
        defaults.simulatorChaosFailureRate(),
        defaults.simulatorChaosStartHourUtc(),
        defaults.simulatorChaosEndHourUtc(),
        defaults.dailyBudgetUsd(),
        defaults.budgetAlertPct(),
        defaults.providerTimeout(),
        defaults.openAiApiKey(),
        defaults.openAiApiKeySecretArn(),
        defaults.promptEnhancementEnabled(),
        defaults.maxStageAttempts(),
        defaults.maxProviderAttempts(),
        defaults.secretCacheTtlMillis(),
        maxPollAttempts);
  }

  private static GenerationRuntimeConfig configWithDailyBudget(double dailyBudgetUsd) {
    GenerationRuntimeConfig defaults = GenerationRuntimeConfig.simulatorDefaults();
    return new GenerationRuntimeConfig(
        defaults.provider(),
        defaults.moderationProvider(),
        defaults.audioOverviewProvider(),
        defaults.llmProvider(),
        defaults.region(),
        defaults.model(),
        defaults.llmModel(),
        defaults.simulatorKind(),
        defaults.simulatorMeanDurationMs(),
        defaults.simulatorColdStartMs(),
        defaults.simulatorFailureRate(),
        defaults.simulatorChaosBusinessHoursEnabled(),
        defaults.simulatorChaosFailureRate(),
        defaults.simulatorChaosStartHourUtc(),
        defaults.simulatorChaosEndHourUtc(),
        dailyBudgetUsd,
        defaults.budgetAlertPct(),
        defaults.providerTimeout(),
        defaults.openAiApiKey(),
        defaults.openAiApiKeySecretArn(),
        defaults.promptEnhancementEnabled(),
        defaults.maxStageAttempts(),
        defaults.maxProviderAttempts(),
        defaults.secretCacheTtlMillis(),
        defaults.maxPollAttempts());
  }

  private static GenerationRuntimeConfig configWithAudioOverviewProvider(GenerationRuntimeConfig defaults, String provider) {
    return new GenerationRuntimeConfig(
        defaults.provider(),
        defaults.moderationProvider(),
        provider,
        defaults.llmProvider(),
        defaults.region(),
        defaults.model(),
        defaults.llmModel(),
        defaults.simulatorKind(),
        defaults.simulatorMeanDurationMs(),
        defaults.simulatorColdStartMs(),
        defaults.simulatorFailureRate(),
        defaults.simulatorChaosBusinessHoursEnabled(),
        defaults.simulatorChaosFailureRate(),
        defaults.simulatorChaosStartHourUtc(),
        defaults.simulatorChaosEndHourUtc(),
        defaults.dailyBudgetUsd(),
        defaults.budgetAlertPct(),
        defaults.providerTimeout(),
        defaults.openAiApiKey(),
        defaults.openAiApiKeySecretArn(),
        defaults.promptEnhancementEnabled(),
        defaults.maxStageAttempts(),
        defaults.maxProviderAttempts(),
        defaults.secretCacheTtlMillis(),
        defaults.maxPollAttempts());
  }

  private static class QueuePublisher implements GenerationEventPublisher {
    private final ArrayDeque<GenerationStageMessage> messages = new ArrayDeque<>();

    @Override
    public void publish(GenerationStageMessage message) {
      messages.add(message);
    }
  }

  private static class MemoryStorage implements GeneratedAssetStorage {
    private final Map<String, byte[]> bytesByAsset = new HashMap<>();
    private int putCount;

    @Override
    public void put(String tenantId, String mediaId, String assetId, Artifact artifact) {
      putCount++;
      bytesByAsset.put(assetId, artifact.bytes());
    }

    @Override
    public String presignedUrl(String tenantId, String mediaId, String assetId, String extension, String downloadName,
        String contentType) {
      return "https://local.test/" + tenantId + "/" + mediaId + "/assets/" + assetId + extension;
    }
  }

  private static class CountingWebhook implements WebhookNotifier {
    private int count;

    @Override
    public void notifyComplete(Media media) {
      count++;
    }
  }

  private static class RecordingMetrics implements GenerationMetrics {
    private int admissionRejected;
    private int safetyDecisions;

    @Override
    public void recordStageLatency(GenerationJob job, GenerationStage stage, Duration duration, boolean success, String errorCode) {
    }

    @Override
    public void recordArtifactCost(GenerationJob job, GenerationStage stage, String provider, double usd) {
    }

    @Override
    public void recordBudgetUsage(GenerationJob job, double reservedUsd, double dailyCapUsd) {
    }

    @Override
    public void recordBudgetCommitted(GenerationJob job, double usedUsd) {
    }

    @Override
    public void recordAdmissionVerdict(String tenantId, String tier, String decision, String reason) {
    }

    @Override
    public void recordAdmissionRejected(String tenantId, String reason) {
      admissionRejected++;
    }

    @Override
    public void recordSafetyDecision(GenerationJob job, GenerationStage stage, String gate, boolean allowed,
        String reason, String classifier) {
      safetyDecisions++;
    }

    @Override
    public void recordWatermarkVerification(GenerationJob job, boolean present) {
    }

    @Override
    public void recordStageRetry(GenerationJob job, GenerationStage stage, int nextAttempt, String errorCode) {
    }
  }

  private static class BadAudioDisclosureFactory extends GenerationProviderFactory {
    private BadAudioDisclosureFactory(GenerationRuntimeConfig config) {
      super(configWithAudioOverviewProvider(config, "bad"), null, "media");
    }

    @Override
    public com.mediaservice.providers.generation.audio.AudioOverviewProvider audioOverviewProvider() {
      return spec -> new Artifact(new byte[] {1, 2, 3}, "audio/wav", ".wav", Map.of("provider", "bad"));
    }

    @Override
    public com.mediaservice.providers.generation.audio.AudioOverviewProvider audioOverviewProvider(String name) {
      return audioOverviewProvider();
    }
  }

  private static class Mp3AudioOverviewFactory extends GenerationProviderFactory {
    private Mp3AudioOverviewFactory(GenerationRuntimeConfig config) {
      super(configWithAudioOverviewProvider(config, "mp3"), null, "media");
    }

    @Override
    public com.mediaservice.providers.generation.audio.AudioOverviewProvider audioOverviewProvider() {
      return spec -> new Artifact("ID3 AI-generated audio".getBytes(), "audio/mpeg", ".mp3", Map.of(
          "provider", "mp3",
          "is_ai_generated", "true",
          "disclosure", "AI-generated audio"));
    }

    @Override
    public com.mediaservice.providers.generation.audio.AudioOverviewProvider audioOverviewProvider(String name) {
      return audioOverviewProvider();
    }
  }

  private static class M4aAudioOverviewFactory extends GenerationProviderFactory {
    private M4aAudioOverviewFactory(GenerationRuntimeConfig config) {
      super(configWithAudioOverviewProvider(config, "m4a"), null, "media");
    }

    @Override
    public com.mediaservice.providers.generation.audio.AudioOverviewProvider audioOverviewProvider() {
      return spec -> new Artifact(new byte[] {
          0, 0, 0, 24,
          'f', 't', 'y', 'p',
          'd', 'a', 's', 'h',
          0, 0, 0, 0,
          'i', 's', 'o', '6',
          'm', 'p', '4', '1'
      }, "audio/mp4", ".m4a", Map.of(
          "provider", "m4a",
          "is_ai_generated", "true",
          "disclosure", "AI-generated audio"));
    }

    @Override
    public com.mediaservice.providers.generation.audio.AudioOverviewProvider audioOverviewProvider(String name) {
      return audioOverviewProvider();
    }
  }

  private static class BadImageSafetyFactory extends GenerationProviderFactory {
    private BadImageSafetyFactory(GenerationRuntimeConfig config) {
      super(config, null, "media");
    }

    @Override
    public com.mediaservice.providers.generation.image.ImageProvider imageProvider() {
      return new com.mediaservice.providers.generation.image.ImageProvider() {
        @Override
        public com.mediaservice.common.generation.provider.ProviderKind kind() {
          return com.mediaservice.common.generation.provider.ProviderKind.SYNC;
        }

        @Override
        public Artifact generateSync(com.mediaservice.common.generation.provider.JobSpec spec) {
          return new Artifact(new byte[] {1, 2, 3}, "image/png", ".png", Map.of("watermark", "present"));
        }

        @Override
        public com.mediaservice.common.generation.provider.ProviderJobId submitAsync(
            com.mediaservice.common.generation.provider.JobSpec spec) {
          throw new UnsupportedOperationException();
        }

        @Override
        public com.mediaservice.common.generation.provider.ProviderState poll(
            com.mediaservice.common.generation.provider.ProviderJobId providerJobId) {
          throw new UnsupportedOperationException();
        }

        @Override
        public Artifact fetch(com.mediaservice.common.generation.provider.ProviderJobId providerJobId) {
          throw new UnsupportedOperationException();
        }
      };
    }
  }

  private static class MemoryRepository implements GenerationRepository {
    private final Map<String, GenerationJob> jobs = new HashMap<>();
    private final Map<String, Media> media = new HashMap<>();
    private final Map<String, MediaAsset> assets = new HashMap<>();
    private final Set<String> claimed = new HashSet<>();
    private final Map<String, String> sideEffectResults = new HashMap<>();
    private final List<GenerationStage> stageRuns = new ArrayList<>();
    private final List<ModerationResult> safetyDecisions = new ArrayList<>();
    private final List<GenerationArtifact> artifacts = new ArrayList<>();
    private boolean budgetReserved;
    private boolean budgetCommitted;
    private boolean budgetReleased;

    @Override
    public void createJob(GenerationJob job, Media media, MediaAsset initialAsset) {
      jobs.put(job.getJobId(), job);
      this.media.put(media.getMediaId(), media);
      assets.put(media.getMediaId(), initialAsset);
    }

    @Override
    public Optional<GenerationJob> getJob(String jobId) {
      return Optional.ofNullable(jobs.get(jobId));
    }

    @Override
    public Optional<Media> getMedia(String mediaId) {
      return Optional.ofNullable(media.get(mediaId));
    }

    @Override
    public Optional<MediaAsset> getAsset(String mediaId, String assetId) {
      MediaAsset asset = assets.get(mediaId);
      return asset != null && assetId.equals(asset.getAssetId()) ? Optional.of(asset) : Optional.empty();
    }

    @Override
    public void updateJobStage(String jobId, GenerationStatus status, GenerationStage stage) {
      GenerationJob job = jobs.get(jobId);
      job.setStatus(status);
      job.setCurrentStage(stage);
      job.setUpdatedAt(Instant.now());
    }

    @Override
    public void updateEnhancedPrompt(String jobId, String enhancedPrompt) {
      jobs.get(jobId).setEnhancedPrompt(enhancedPrompt);
    }

    @Override
    public void recordProviderJobId(String jobId, String providerJobId) {
      jobs.get(jobId).setProviderJobId(providerJobId);
    }

    @Override
    public void recordResultArtifact(String jobId, String assetId, String contentType, String extension, long sizeBytes) {
      GenerationJob job = jobs.get(jobId);
      job.setResultAssetId(assetId);
      job.setResultContentType(contentType);
      job.setResultExtension(extension);
      job.setResultSizeBytes(sizeBytes);
    }

    @Override
    public void completeJob(String jobId) {
      GenerationJob job = jobs.get(jobId);
      job.setStatus(GenerationStatus.COMPLETE);
      job.setCurrentStage(GenerationStage.DELIVERY);
      job.setCompletedAt(Instant.now());
    }

    @Override
    public void failJob(String jobId, String code, String message) {
      GenerationJob job = jobs.get(jobId);
      job.setStatus(GenerationStatus.FAILED);
      job.setErrorCode(code);
      job.setErrorMessage(message);
    }

    @Override
    public void updateMediaStatus(String mediaId, MediaStatus status) {
      media.get(mediaId).setStatus(status);
    }

    @Override
    public void updateGeneratedMediaComplete(String mediaId, long size, String contentType, String extension) {
      Media item = media.get(mediaId);
      item.setStatus(MediaStatus.COMPLETE);
      item.setSize(size);
      item.setMimetype(contentType);
      item.setName(mediaId + "-generated" + extension);
    }

    @Override
    public void updateAssetComplete(String mediaId, String assetId, long size, String contentType, String extension) {
      MediaAsset asset = assets.get(mediaId);
      asset.setStatus(AssetStatus.COMPLETE);
      asset.setSize(size);
      asset.setMimetype(contentType);
      asset.setOutputFormat(extension.replace(".", ""));
      asset.setDownloadName(mediaId + "-generated" + extension);
    }

    @Override
    public void createArtifact(GenerationArtifact artifact) {
      artifacts.add(artifact);
    }

    @Override
    public void createStageRun(String tenantId, String jobId, GenerationStage stage, int attempt,
        GenerationStatus status, String errorCode) {
      stageRuns.add(stage);
    }

    @Override
    public void createSafetyDecision(String tenantId, String jobId, GenerationStage stage, String gate,
        ModerationResult result) {
      safetyDecisions.add(result);
    }

    @Override
    public boolean claimStageSideEffect(String jobId, GenerationStage stage) {
      return claimed.add(jobId + ":" + stage);
    }

    @Override
    public Optional<String> getStageSideEffectResult(String jobId, GenerationStage stage) {
      return Optional.ofNullable(sideEffectResults.get(jobId + ":" + stage));
    }

    @Override
    public void completeStageSideEffect(String jobId, GenerationStage stage, String resultRef) {
      sideEffectResults.put(jobId + ":" + stage, resultRef != null ? resultRef : "ok");
    }

    @Override
    public void failStageSideEffect(String jobId, GenerationStage stage, String errorCode) {
      claimed.remove(jobId + ":" + stage);
      sideEffectResults.remove(jobId + ":" + stage);
    }

    @Override
    public void reserveBudget(String tenantId, String jobId, double estimatedUsd, double dailyCapUsd) {
      if (estimatedUsd > dailyCapUsd) {
        throw new GenerationProviderException("BUDGET_EXCEEDED", "Daily generation budget exceeded");
      }
      budgetReserved = true;
    }

    @Override
    public void commitBudget(String tenantId, String jobId, double estimatedUsd, double actualUsd) {
      budgetCommitted = true;
    }

    @Override
    public void releaseBudget(String tenantId, String jobId) {
      budgetReleased = true;
    }
  }

  private static class InFlightPreprocessRepository extends MemoryRepository {
    @Override
    public boolean claimStageSideEffect(String jobId, GenerationStage stage) {
      if (stage == GenerationStage.PREPROCESS) {
        return false;
      }
      return super.claimStageSideEffect(jobId, stage);
    }
  }

  /**
   * Repository that simulates a stage that already entered unknown_outcome before this worker
   * arrived: the first {@code INFERENCE} claim always returns Reconcile so the workflow has to
   * call {@code provider.reconcile(...)}. Subsequent claims behave normally.
   */
  private static class UnknownOutcomeRepository extends MemoryRepository {
    private final boolean injectOnInference;
    private boolean inferenceConsumed;
    private int reconcileCalls;

    private UnknownOutcomeRepository(boolean injectOnInference) {
      this.injectOnInference = injectOnInference;
    }

    @Override
    public ClaimOutcome claimStageSideEffectV2(String jobId, GenerationStage stage) {
      if (injectOnInference && stage == GenerationStage.INFERENCE && !inferenceConsumed) {
        inferenceConsumed = true;
        return new ClaimOutcome.Reconcile("IDEMPOTENCY#INFERENCE#provider_call");
      }
      return super.claimStageSideEffectV2(jobId, stage);
    }
  }

  /**
   * Image provider whose {@code reconcile} returns either a synthetic artifact (reconciliation
   * succeeds) or empty (reconciliation fails — caller must mark unrecoverable).
   */
  private static class ReconcilableImageFactory extends GenerationProviderFactory {
    private final boolean recoverable;

    private ReconcilableImageFactory(GenerationRuntimeConfig config, boolean recoverable) {
      super(config, null, "media");
      this.recoverable = recoverable;
    }

    @Override
    public com.mediaservice.providers.generation.image.ImageProvider imageProvider() {
      return new ReconcilableImageProvider(recoverable);
    }
  }

  /** Sync image provider that exposes reconcile(); records call counts on the active repo. */
  private static class ReconcilableImageProvider implements ImageProvider {
    private final boolean recoverable;

    private ReconcilableImageProvider(boolean recoverable) {
      this.recoverable = recoverable;
    }

    @Override
    public ProviderKind kind() {
      return ProviderKind.SYNC;
    }

    @Override
    public Artifact generateSync(JobSpec spec) {
      return pngArtifact("fresh");
    }

    @Override
    public ProviderJobId submitAsync(JobSpec spec) {
      throw new UnsupportedOperationException();
    }

    @Override
    public ProviderState poll(ProviderJobId providerJobId) {
      throw new UnsupportedOperationException();
    }

    @Override
    public Artifact fetch(ProviderJobId providerJobId) {
      throw new UnsupportedOperationException();
    }

    @Override
    public Optional<Artifact> reconcile(String clientRequestId) {
      ActiveReconcileCounter.bump();
      return recoverable ? Optional.of(pngArtifact("reconciled")) : Optional.empty();
    }

    private static Artifact pngArtifact(String tag) {
      byte[] signature = new byte[] {(byte) 0x89, 'P', 'N', 'G', 13, 10, 26, 10};
      byte[] payload = tag.getBytes(java.nio.charset.StandardCharsets.UTF_8);
      byte[] bytes = new byte[signature.length + payload.length];
      System.arraycopy(signature, 0, bytes, 0, signature.length);
      System.arraycopy(payload, 0, bytes, signature.length, payload.length);
      return new Artifact(bytes, "image/png", ".png", Map.of(
          "watermark", "simulated",
          "content_safety", "simulated",
          "provider", "reconcilable"));
    }
  }

  /** Test-scoped global counter so the provider can update the active repository. */
  private static final class ActiveReconcileCounter {
    static UnknownOutcomeRepository active;

    static void bump() {
      if (active != null) {
        active.reconcileCalls++;
      }
    }
  }

  /**
   * Repository that mirrors the production budget aggregate-cap behaviour: pending_usd +
   * committed_usd_cache must not exceed the cap; release/commit decrement pending.
   */
  private static class AggregateBudgetRepository extends MemoryRepository {
    private double pendingUsd;
    private double committedUsd;
    private final double cap;
    private final Map<String, Double> reservations = new HashMap<>();

    private AggregateBudgetRepository(double cap) {
      this.cap = cap;
    }

    @Override
    public synchronized void reserveBudget(String tenantId, String jobId, double estimatedUsd, double dailyCapUsd) {
      if (estimatedUsd > cap) {
        throw new GenerationProviderException("BUDGET_EXCEEDED", "estimate above cap");
      }
      if (pendingUsd + committedUsd + estimatedUsd > cap + 1e-9) {
        throw new GenerationProviderException("BUDGET_EXCEEDED", "Daily generation budget exceeded");
      }
      pendingUsd += estimatedUsd;
      reservations.put(jobId, estimatedUsd);
    }

    @Override
    public synchronized void commitBudget(String tenantId, String jobId, double estimatedUsd, double actualUsd) {
      Double reserved = reservations.get(jobId);
      if (reserved == null) return;
      pendingUsd -= reserved;
      committedUsd += actualUsd;
      reservations.remove(jobId);
    }

    @Override
    public synchronized void releaseBudget(String tenantId, String jobId) {
      Double reserved = reservations.remove(jobId);
      if (reserved == null) return;
      pendingUsd -= reserved;
    }
  }

  /** Async-kind image provider that always reports RUNNING, forcing poll-cap exhaustion. */
  private static class AlwaysRunningAsyncFactory extends GenerationProviderFactory {
    private AlwaysRunningAsyncFactory(GenerationRuntimeConfig config) {
      super(config, null, "media");
    }

    @Override
    public ImageProvider imageProvider() {
      return new ImageProvider() {
        @Override
        public ProviderKind kind() {
          return ProviderKind.ASYNC;
        }

        @Override
        public Artifact generateSync(JobSpec spec) {
          throw new UnsupportedOperationException();
        }

        @Override
        public ProviderJobId submitAsync(JobSpec spec) {
          return new ProviderJobId("forever-running-" + spec.jobId());
        }

        @Override
        public ProviderState poll(ProviderJobId providerJobId) {
          return new ProviderState(ProviderStatus.RUNNING, "still working");
        }

        @Override
        public Artifact fetch(ProviderJobId providerJobId) {
          throw new UnsupportedOperationException();
        }
      };
    }
  }

  /**
   * Minimal {@link java.net.http.HttpClient} stand-in that returns a queued list of replies.
   * Records every HttpRequest so tests can inspect headers (Idempotency-Key, etc.).
   */
  private static class FakeHttpClient extends java.net.http.HttpClient {
    final java.util.ArrayDeque<Reply> responses = new java.util.ArrayDeque<>();
    int calls;
    java.net.http.HttpRequest lastRequest;

    record Reply(int statusCode, String body) {
    }

    @Override public java.util.Optional<java.net.CookieHandler> cookieHandler() { return java.util.Optional.empty(); }
    @Override public java.util.Optional<java.time.Duration> connectTimeout() { return java.util.Optional.empty(); }
    @Override public java.net.http.HttpClient.Redirect followRedirects() { return java.net.http.HttpClient.Redirect.NEVER; }
    @Override public java.util.Optional<java.net.ProxySelector> proxy() { return java.util.Optional.empty(); }
    @Override public javax.net.ssl.SSLContext sslContext() {
      try { return javax.net.ssl.SSLContext.getDefault(); }
      catch (Exception e) { throw new RuntimeException(e); }
    }
    @Override public javax.net.ssl.SSLParameters sslParameters() { return new javax.net.ssl.SSLParameters(); }
    @Override public java.util.Optional<java.net.Authenticator> authenticator() { return java.util.Optional.empty(); }
    @Override public java.net.http.HttpClient.Version version() { return java.net.http.HttpClient.Version.HTTP_1_1; }
    @Override public java.util.Optional<java.util.concurrent.Executor> executor() { return java.util.Optional.empty(); }

    @SuppressWarnings("unchecked")
    @Override
    public <T> java.net.http.HttpResponse<T> send(java.net.http.HttpRequest request,
        java.net.http.HttpResponse.BodyHandler<T> responseBodyHandler) {
      calls++;
      lastRequest = request;
      Reply reply = responses.isEmpty() ? new Reply(200, "{}") : responses.removeFirst();
      return (java.net.http.HttpResponse<T>) new FakeResponse<>(request, reply, responseBodyHandler);
    }

    @Override
    public <T> java.util.concurrent.CompletableFuture<java.net.http.HttpResponse<T>> sendAsync(
        java.net.http.HttpRequest request, java.net.http.HttpResponse.BodyHandler<T> responseBodyHandler) {
      return java.util.concurrent.CompletableFuture.completedFuture(send(request, responseBodyHandler));
    }

    @Override
    public <T> java.util.concurrent.CompletableFuture<java.net.http.HttpResponse<T>> sendAsync(
        java.net.http.HttpRequest request, java.net.http.HttpResponse.BodyHandler<T> responseBodyHandler,
        java.net.http.HttpResponse.PushPromiseHandler<T> pushPromiseHandler) {
      return sendAsync(request, responseBodyHandler);
    }
  }

  private static class FakeResponse<T> implements java.net.http.HttpResponse<T> {
    private final java.net.http.HttpRequest request;
    private final FakeHttpClient.Reply reply;
    private final T body;

    FakeResponse(java.net.http.HttpRequest request, FakeHttpClient.Reply reply,
        java.net.http.HttpResponse.BodyHandler<T> handler) {
      this.request = request;
      this.reply = reply;
      byte[] raw = reply.body().getBytes(java.nio.charset.StandardCharsets.UTF_8);
      try {
        java.net.http.HttpResponse.BodySubscriber<T> bs = handler.apply(new java.net.http.HttpResponse.ResponseInfo() {
          @Override public int statusCode() { return reply.statusCode(); }
          @Override public java.net.http.HttpHeaders headers() {
            return java.net.http.HttpHeaders.of(Map.of(), (a, b) -> true);
          }
          @Override public java.net.http.HttpClient.Version version() {
            return java.net.http.HttpClient.Version.HTTP_1_1;
          }
        });
        java.util.concurrent.Flow.Subscription noop = new java.util.concurrent.Flow.Subscription() {
          @Override public void request(long n) { }
          @Override public void cancel() { }
        };
        bs.onSubscribe(noop);
        bs.onNext(java.util.List.of(java.nio.ByteBuffer.wrap(raw)));
        bs.onComplete();
        this.body = bs.getBody().toCompletableFuture().get();
      } catch (Exception e) {
        throw new RuntimeException(e);
      }
    }

    @Override public int statusCode() { return reply.statusCode(); }
    @Override public java.net.http.HttpRequest request() { return request; }
    @Override public java.util.Optional<java.net.http.HttpResponse<T>> previousResponse() { return java.util.Optional.empty(); }
    @Override public java.net.http.HttpHeaders headers() {
      return java.net.http.HttpHeaders.of(Map.of(), (a, b) -> true);
    }
    @Override public T body() { return body; }
    @Override public java.util.Optional<javax.net.ssl.SSLSession> sslSession() { return java.util.Optional.empty(); }
    @Override public java.net.URI uri() { return request.uri(); }
    @Override public java.net.http.HttpClient.Version version() { return java.net.http.HttpClient.Version.HTTP_1_1; }
  }

  /** Repository whose idempotency rows can be marked terminal-failed so re-claim returns failure. */
  private static class FailedRowRepository extends MemoryRepository {
    final java.util.HashSet<String> failedStages = new java.util.HashSet<>();

    @Override
    public ClaimOutcome claimStageSideEffectV2(String jobId, GenerationStage stage) {
      if (failedStages.contains(jobId + ":" + stage)) {
        return new ClaimOutcome.TerminalFailure("STAGE_FAILED", "stage previously failed");
      }
      return super.claimStageSideEffectV2(jobId, stage);
    }
  }
}
