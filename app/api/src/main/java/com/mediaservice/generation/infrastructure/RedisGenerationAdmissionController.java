package com.mediaservice.generation.infrastructure;

import com.mediaservice.common.generation.GenerationJob;
import com.mediaservice.common.generation.Tier;
import com.mediaservice.providers.generation.core.AdmissionVerdict;
import com.mediaservice.providers.generation.core.GenerationAdmissionController;
import com.mediaservice.providers.generation.core.GenerationSubmission;
import java.time.Duration;
import java.time.LocalDate;
import java.time.YearMonth;
import java.time.ZoneOffset;
import java.util.Collections;
import java.util.List;
import java.util.Map;
import org.springframework.data.redis.core.StringRedisTemplate;
import org.springframework.data.redis.core.script.DefaultRedisScript;

public class RedisGenerationAdmissionController implements GenerationAdmissionController {
  private static final DefaultRedisScript<Long> INCR_WITH_TTL_SCRIPT;
  /** Auto-pause for the free tier triggers when the resource controller returns DEGRADED. */
  private static final Duration AUTO_PAUSE_TTL = Duration.ofSeconds(60);

  static {
    INCR_WITH_TTL_SCRIPT = new DefaultRedisScript<>(
        "local current = redis.call('INCR', KEYS[1])\n"
            + "if current == 1 then redis.call('EXPIRE', KEYS[1], ARGV[1]) end\n"
            + "return current",
        Long.class);
  }

  private final StringRedisTemplate redisTemplate;
  private final GenerationAdmissionController resourceController;
  private final int freeDailyLimit;
  private final int paidDailyLimit;
  private final int freeMonthlyLimit;
  private final int paidMonthlyLimit;
  private final int freeOutstandingLimit;
  private final int paidOutstandingLimit;

  public RedisGenerationAdmissionController(StringRedisTemplate redisTemplate,
      GenerationAdmissionController resourceController,
      int freeDailyLimit,
      int paidDailyLimit,
      int freeMonthlyLimit,
      int paidMonthlyLimit,
      int freeOutstandingLimit,
      int paidOutstandingLimit) {
    this.redisTemplate = redisTemplate;
    this.resourceController = resourceController;
    this.freeDailyLimit = freeDailyLimit;
    this.paidDailyLimit = paidDailyLimit;
    this.freeMonthlyLimit = freeMonthlyLimit;
    this.paidMonthlyLimit = paidMonthlyLimit;
    this.freeOutstandingLimit = freeOutstandingLimit;
    this.paidOutstandingLimit = paidOutstandingLimit;
  }

  @Override
  public AdmissionVerdict evaluate(GenerationSubmission submission) {
    String tier = normalizeTier(submission.tier());
    String tenantId = submission.tenantId();

    // One Redis round-trip for every per-submission read instead of 7 sequential GETs.
    String regionKey = "generation:region:failover:state";
    String pauseKey = controlKey("pause", tier);
    String abuseKey = tenantKey(tenantId, "abuse");
    String balanceKey = tenantKey(tenantId, "balance_usd");
    String dailyKey = counterKey(tenantId, tier, "daily", LocalDate.now(ZoneOffset.UTC).toString());
    String monthlyKey = counterKey(tenantId, tier, "monthly", YearMonth.now(ZoneOffset.UTC).toString());
    String outstandingKey = counterKey(tenantId, tier, "outstanding", "current");
    List<String> values = redisTemplate.opsForValue().multiGet(
        List.of(regionKey, pauseKey, abuseKey, balanceKey, dailyKey, monthlyKey, outstandingKey));
    String regionState = values.get(0);
    boolean tierPaused = parseBool(values.get(1));
    boolean abuseFlag = parseBool(values.get(2));
    String balance = values.get(3);
    int dailyCount = parseInt(values.get(4));
    int monthlyCount = parseInt(values.get(5));
    int outstanding = parseInt(values.get(6));

    if ("FAILED_OVER".equalsIgnoreCase(regionState)) {
      return AdmissionVerdict.reject("ADMISSION_REGION_FAILED_OVER",
          "Generation primary region has failed over; retry after the secondary region accepts traffic", 300);
    }
    if (tierPaused) {
      return AdmissionVerdict.reject("ADMISSION_TIER_PAUSED",
          "Generation tier is paused by admission control", 60);
    }
    if (abuseFlag) {
      return AdmissionVerdict.reject("ADMISSION_ABUSE_SIGNAL",
          "Generation admission blocked by abuse signal", 300);
    }
    if (balance != null && !balance.isBlank() && Double.parseDouble(balance) < 0.0) {
      return AdmissionVerdict.reject("ADMISSION_BALANCE_REQUIRED",
          "Generation admission requires a non-negative account balance", 300);
    }
    if (dailyCount >= dailyLimit(tier)) {
      return AdmissionVerdict.reject("ADMISSION_DAILY_QUOTA_EXCEEDED",
          "Generation daily quota exceeded for tier " + tier, 86400);
    }
    if (monthlyCount >= monthlyLimit(tier)) {
      return AdmissionVerdict.reject("ADMISSION_MONTHLY_QUOTA_EXCEEDED",
          "Generation monthly quota exceeded for tier " + tier, 86400);
    }
    if (outstanding >= outstandingLimit(tier)) {
      return AdmissionVerdict.acceptedDelayed("ADMISSION_OUTSTANDING_LIMIT",
          "Generation accepted with elevated wait time because outstanding jobs are at tier pressure",
          60,
          metadata(tier, dailyCount, monthlyCount, outstanding));
    }

    AdmissionVerdict resourceVerdict = resourceController.evaluate(submission);
    if (!resourceVerdict.allowed()) {
      return resourceVerdict;
    }
    // Auto-pause the free tier when the resource controller signals DEGRADED — sheds future
    // free-tier load until the short TTL expires so paid traffic is protected without an
    // operator in the loop. The current submission still proceeds with the DEGRADED verdict.
    if (resourceVerdict.decision() == AdmissionVerdict.Decision.DEGRADED) {
      redisTemplate.opsForValue().set(controlKey("pause", Tier.FREE.wireValue()), "true", AUTO_PAUSE_TTL);
      redisTemplate.opsForValue().set(controlKey("auto_pause_reason", Tier.FREE.wireValue()),
          resourceVerdict.code() != null ? resourceVerdict.code() : "ADMISSION_DEGRADED", AUTO_PAUSE_TTL);
    }
    Map<String, String> metadata = merge(resourceVerdict.metadata(), metadata(tier, dailyCount, monthlyCount, outstanding));
    return switch (resourceVerdict.decision()) {
      case ACCEPTED_DELAYED -> AdmissionVerdict.acceptedDelayed(resourceVerdict.code(), resourceVerdict.message(),
          resourceVerdict.retryAfterSeconds(), metadata);
      case DEGRADED -> AdmissionVerdict.degraded(resourceVerdict.code(), resourceVerdict.message(),
          resourceVerdict.retryAfterSeconds(), metadata);
      case REJECTED -> resourceVerdict;
      case ACCEPTED -> AdmissionVerdict.allow(metadata);
    };
  }

  @Override
  public void recordAccepted(GenerationJob job) {
    String tier = normalizeTier(job.getTier());
    increment(counterKey(job.getTenantId(), tier, "daily", LocalDate.now(ZoneOffset.UTC).toString()), Duration.ofDays(2));
    increment(counterKey(job.getTenantId(), tier, "monthly", YearMonth.now(ZoneOffset.UTC).toString()), Duration.ofDays(35));
    increment(counterKey(job.getTenantId(), tier, "outstanding", "current"), Duration.ofDays(2));
  }

  @Override
  public void release(GenerationJob job) {
    if (job == null) {
      return;
    }
    String key = counterKey(job.getTenantId(), normalizeTier(job.getTier()), "outstanding", "current");
    Long value = redisTemplate.opsForValue().decrement(key);
    if (value != null && value < 0) {
      redisTemplate.opsForValue().set(key, "0", Duration.ofDays(2));
    }
  }

  @Override
  public void rollback(GenerationJob job) {
    if (job == null) {
      return;
    }
    String tier = normalizeTier(job.getTier());
    decrementFloorZero(counterKey(job.getTenantId(), tier, "daily", LocalDate.now(ZoneOffset.UTC).toString()));
    decrementFloorZero(counterKey(job.getTenantId(), tier, "monthly", YearMonth.now(ZoneOffset.UTC).toString()));
    release(job);
  }

  private void increment(String key, Duration ttl) {
    // Atomic INCR + EXPIRE-on-first-increment via Lua. Guarantees TTL is only set
    // on creation so we never reset a sliding window mid-bucket.
    redisTemplate.execute(INCR_WITH_TTL_SCRIPT, Collections.singletonList(key), String.valueOf(ttl.toSeconds()));
  }

  private void decrementFloorZero(String key) {
    Long value = redisTemplate.opsForValue().decrement(key);
    if (value != null && value < 0) {
      redisTemplate.opsForValue().set(key, "0");
    }
  }

  private int dailyLimit(String tier) {
    return Tier.PAID.wireValue().equals(tier) ? paidDailyLimit : freeDailyLimit;
  }

  private int monthlyLimit(String tier) {
    return Tier.PAID.wireValue().equals(tier) ? paidMonthlyLimit : freeMonthlyLimit;
  }

  private int outstandingLimit(String tier) {
    return Tier.PAID.wireValue().equals(tier) ? paidOutstandingLimit : freeOutstandingLimit;
  }

  private int intValue(String key) {
    return parseInt(redisTemplate.opsForValue().get(key));
  }

  private boolean bool(String key) {
    return parseBool(redisTemplate.opsForValue().get(key));
  }

  private static int parseInt(String value) {
    return value == null || value.isBlank() ? 0 : Integer.parseInt(value);
  }

  private static boolean parseBool(String value) {
    return Boolean.parseBoolean(value);
  }

  private Map<String, String> metadata(String tier, int dailyCount, int monthlyCount, int outstanding) {
    return Map.of(
        "tier", tier,
        "daily_count", String.valueOf(dailyCount),
        "daily_limit", String.valueOf(dailyLimit(tier)),
        "monthly_count", String.valueOf(monthlyCount),
        "monthly_limit", String.valueOf(monthlyLimit(tier)),
        "outstanding_count", String.valueOf(outstanding),
        "outstanding_limit", String.valueOf(outstandingLimit(tier)));
  }

  private Map<String, String> merge(Map<String, String> left, Map<String, String> right) {
    java.util.HashMap<String, String> merged = new java.util.HashMap<>();
    if (left != null) {
      merged.putAll(left);
    }
    merged.putAll(right);
    return merged;
  }

  private String normalizeTier(String tier) {
    return Tier.fromString(tier).wireValue();
  }

  private String controlKey(String kind, String value) {
    return "generation:admission:control:" + kind + ":" + value;
  }

  private String tenantKey(String tenantId, String kind) {
    return "generation:admission:tenant:" + tenantId + ":" + kind;
  }

  private String counterKey(String tenantId, String tier, String scope, String bucket) {
    return "generation:admission:" + scope + ":" + tenantId + ":" + tier + ":" + bucket;
  }
}
