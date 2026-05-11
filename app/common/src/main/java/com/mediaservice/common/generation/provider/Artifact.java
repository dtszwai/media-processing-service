package com.mediaservice.common.generation.provider;

import java.util.Map;

public record Artifact(
    byte[] bytes,
    String contentType,
    String extension,
    Map<String, String> metadata
) {
}
