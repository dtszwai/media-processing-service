package com.mediaservice.common.generation.provider;

import com.mediaservice.common.generation.GenerationOutputType;
import java.util.Map;

public record JobSpec(
    String jobId,
    String mediaId,
    String tenantId,
    GenerationOutputType outputType,
    String prompt,
    String model,
    String resolution,
    Long seed,
    Map<String, String> metadata
) {
}
