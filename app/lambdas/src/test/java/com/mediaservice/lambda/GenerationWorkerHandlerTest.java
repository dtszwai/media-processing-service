package com.mediaservice.lambda;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import com.amazonaws.services.lambda.runtime.events.SQSEvent;
import com.fasterxml.jackson.databind.ObjectMapper;
import com.mediaservice.common.generation.GenerationStage;
import com.mediaservice.common.generation.GenerationStageMessage;
import com.mediaservice.providers.generation.core.GenerationWorkflow;
import java.util.List;
import org.junit.jupiter.api.Test;

class GenerationWorkerHandlerTest {
  @Test
  void handlesRawGenerationStageMessages() {
    CapturingWorkflow workflow = new CapturingWorkflow();
    GenerationWorkerHandler handler = new GenerationWorkerHandler(workflow, new ObjectMapper());
    SQSEvent.SQSMessage record = new SQSEvent.SQSMessage();
    record.setMessageId("msg-1");
    record.setBody("{\"job_id\":\"gen_123\",\"stage\":\"INFERENCE\",\"attempt\":2}");
    SQSEvent event = new SQSEvent();
    event.setRecords(List.of(record));

    handler.handleRequest(event, null);

    assertThat(workflow.message.getJobId()).isEqualTo("gen_123");
    assertThat(workflow.message.getStage()).isEqualTo(GenerationStage.INFERENCE);
    assertThat(workflow.message.getAttempt()).isEqualTo(2);
  }

  @Test
  void toleratesSnsEnvelopeDuringLocalDebugging() {
    CapturingWorkflow workflow = new CapturingWorkflow();
    GenerationWorkerHandler handler = new GenerationWorkerHandler(workflow, new ObjectMapper());
    SQSEvent.SQSMessage record = new SQSEvent.SQSMessage();
    record.setMessageId("msg-1");
    record.setBody("{\"Message\":\"{\\\"job_id\\\":\\\"gen_456\\\",\\\"stage\\\":\\\"DELIVERY\\\",\\\"attempt\\\":1}\"}");
    SQSEvent event = new SQSEvent();
    event.setRecords(List.of(record));

    handler.handleRequest(event, null);

    assertThat(workflow.message.getJobId()).isEqualTo("gen_456");
    assertThat(workflow.message.getStage()).isEqualTo(GenerationStage.DELIVERY);
  }

  @Test
  void propagatesExceptionSoSqsDoesNotDeleteMessage() {
    ThrowingWorkflow workflow = new ThrowingWorkflow();
    GenerationWorkerHandler handler = new GenerationWorkerHandler(workflow, new ObjectMapper());
    SQSEvent.SQSMessage record = new SQSEvent.SQSMessage();
    record.setMessageId("msg-fail");
    record.setBody("{\"job_id\":\"gen_err\",\"stage\":\"INFERENCE\",\"attempt\":1}");
    SQSEvent event = new SQSEvent();
    event.setRecords(List.of(record));

    assertThatThrownBy(() -> handler.handleRequest(event, null))
        .isInstanceOf(RuntimeException.class)
        .hasMessageContaining("downstream boom");
  }

  private static class ThrowingWorkflow extends GenerationWorkflow {
    private ThrowingWorkflow() {
      super(null, null, null, null, null);
    }

    @Override
    public void processStage(GenerationStageMessage message) {
      throw new RuntimeException("downstream boom");
    }
  }

  private static class CapturingWorkflow extends GenerationWorkflow {
    private GenerationStageMessage message;

    private CapturingWorkflow() {
      super(null, null, null, null, null);
    }

    @Override
    public void processStage(GenerationStageMessage message) {
      this.message = message;
    }
  }
}
