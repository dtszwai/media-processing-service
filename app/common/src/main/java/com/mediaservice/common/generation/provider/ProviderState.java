package com.mediaservice.common.generation.provider;

public record ProviderState(
    ProviderStatus status,
    String message
) {
}
