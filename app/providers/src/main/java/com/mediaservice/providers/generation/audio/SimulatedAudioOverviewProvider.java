package com.mediaservice.providers.generation.audio;

import com.mediaservice.common.generation.provider.Artifact;
import com.mediaservice.providers.generation.audio.AudioOverviewProvider;
import com.mediaservice.common.generation.provider.JobSpec;
import java.util.Map;

public class SimulatedAudioOverviewProvider implements AudioOverviewProvider {
  public SimulatedAudioOverviewProvider() {
  }

  @Override
  public Artifact generateOverview(JobSpec spec) {
    String marker = "AI-generated audio overview\njob=" + spec.jobId() + "\n";
    byte[] bytes = WavEncoder.encode(8000, 330, 9000, 60_000, marker);
    return new Artifact(bytes, "audio/wav", ".wav", Map.of(
        "provider", "simulated",
        "is_ai_generated", "true",
        "disclosure", "AI-generated audio"));
  }
}
