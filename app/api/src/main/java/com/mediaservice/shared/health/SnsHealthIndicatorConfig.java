package com.mediaservice.shared.health;

import org.springframework.beans.factory.annotation.Value;
import org.springframework.context.annotation.Bean;
import org.springframework.context.annotation.Configuration;
import software.amazon.awssdk.services.sns.SnsClient;

@Configuration
public class SnsHealthIndicatorConfig {

  @Bean("sns")
  public SnsHealthIndicator snsHealthIndicator(
      SnsClient snsClient,
      @Value("${aws.sns.topic-arn:}") String topicArn) {
    return new SnsHealthIndicator(snsClient, topicArn, "SNS");
  }

  @Bean("generationSns")
  public SnsHealthIndicator generationSnsHealthIndicator(
      SnsClient snsClient,
      @Value("${media.generation.topic-arn:}") String topicArn) {
    return new SnsHealthIndicator(snsClient, topicArn, "Generation SNS");
  }
}
