package com.mediaservice.shared.config.properties;

import java.util.ArrayList;
import java.util.List;
import lombok.Data;
import org.springframework.boot.context.properties.ConfigurationProperties;
import org.springframework.stereotype.Component;

@Data
@Component
@ConfigurationProperties(prefix = "short-url")
public class ShortUrlProperties {
  private String baseDomain;
  private int generatedLength = 8;
  private int minAliasLength = 4;
  private int maxAliasLength = 64;
  private List<String> reservedAliases = new ArrayList<>();
}
