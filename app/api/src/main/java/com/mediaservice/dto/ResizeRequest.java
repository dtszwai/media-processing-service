package com.mediaservice.dto;

import jakarta.validation.constraints.Max;
import jakarta.validation.constraints.Min;
import jakarta.validation.constraints.Pattern;
import lombok.AllArgsConstructor;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@NoArgsConstructor
@AllArgsConstructor
public class ResizeRequest {
  @Min(value = 100, message = "Width must be at least 100 pixels")
  @Max(value = 1024, message = "Width must be at most 1024 pixels")
  private Integer width;

  @Pattern(regexp = "^(jpeg|png|webp)?$", message = "outputFormat must be one of: jpeg, png, webp")
  private String outputFormat;
}
