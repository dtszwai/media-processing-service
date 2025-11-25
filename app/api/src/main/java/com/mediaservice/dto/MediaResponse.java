package com.mediaservice.dto;

import com.fasterxml.jackson.annotation.JsonInclude;
import com.mediaservice.model.MediaStatus;
import lombok.AllArgsConstructor;
import lombok.Builder;
import lombok.Data;
import lombok.NoArgsConstructor;

@Data
@Builder
@NoArgsConstructor
@AllArgsConstructor
@JsonInclude(JsonInclude.Include.NON_NULL)
public class MediaResponse {
    private String mediaId;
    private Long size;
    private String name;
    private String mimetype;
    private MediaStatus status;
    private String message;
}
