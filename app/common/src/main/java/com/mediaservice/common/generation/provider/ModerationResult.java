package com.mediaservice.common.generation.provider;

public record ModerationResult(
    boolean allowed,
    String classifier,
    String modelVersion,
    double score,
    String reason
) {
}
