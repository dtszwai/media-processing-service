package com.mediaservice.generation.infrastructure;

import com.mediaservice.common.generation.Tier;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.stereotype.Service;

/** Redis-backed admin operations for admission-control overrides + region failover state. */
@Service
public class AdmissionControlAdminService {
  private static final String REGION_STATE_KEY = "generation:region:failover:state";
  private static final String REGION_REASON_KEY = "generation:region:failover:reason";
  private static final String DEFAULT_REGION_STATE = "PRIMARY_HEALTHY";

  private final StringRedisTemplate redisTemplate;

  public AdmissionControlAdminService(StringRedisTemplate redisTemplate) {
    this.redisTemplate = redisTemplate;
  }

  public record RegionState(String state, String reason) {
    public static RegionState defaults() {
      return new RegionState(DEFAULT_REGION_STATE, "");
    }
  }

  public void setTierPause(String tier, boolean paused) {
    redisTemplate.opsForValue().set("generation:admission:control:pause:"
        + Tier.fromString(tier).wireValue(), String.valueOf(paused));
  }

  public void setTenantAbuse(String tenantId, boolean abuse) {
    redisTemplate.opsForValue().set("generation:admission:tenant:" + tenantId + ":abuse", String.valueOf(abuse));
  }

  public void setTenantBalance(String tenantId, double balanceUsd) {
    redisTemplate.opsForValue().set("generation:admission:tenant:" + tenantId + ":balance_usd",
        String.valueOf(balanceUsd));
  }

  public RegionState getRegionState() {
    String state = redisTemplate.opsForValue().get(REGION_STATE_KEY);
    String reason = redisTemplate.opsForValue().get(REGION_REASON_KEY);
    return new RegionState(state != null ? state : DEFAULT_REGION_STATE, reason != null ? reason : "");
  }

  /**
   * Set or clear region failover state. {@code PRIMARY_HEALTHY} clears the state; other values
   * (DEGRADED, FAILED_OVER) set both state and an optional reason.
   */
  public void setRegionState(String normalizedState, String reason) {
    if (DEFAULT_REGION_STATE.equals(normalizedState)) {
      redisTemplate.delete(REGION_STATE_KEY);
      redisTemplate.delete(REGION_REASON_KEY);
      return;
    }
    redisTemplate.opsForValue().set(REGION_STATE_KEY, normalizedState);
    if (reason != null) {
      redisTemplate.opsForValue().set(REGION_REASON_KEY, reason);
    }
  }
}
