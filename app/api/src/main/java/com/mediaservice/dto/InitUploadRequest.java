package com.mediaservice.dto;

import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.NotBlank;
import jakarta.validation.constraints.Pattern;
import jakarta.validation.constraints.Positive;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
public class InitUploadRequest {
    @NotBlank(message = "fileName is required")
    private String fileName;

    @Positive(message = "fileSize must be positive")
    private long fileSize;

    @NotBlank(message = "contentType is required")
    private String contentType;

    @Min(value = 100, message = "width must be at least 100")
    @Max(value = 1024, message = "width must be at most 1024")
    private Integer width;

    @Pattern(regexp = "^(jpeg|png|webp)?$", message = "outputFormat must be one of: jpeg, png, webp")
    private String outputFormat;
}
