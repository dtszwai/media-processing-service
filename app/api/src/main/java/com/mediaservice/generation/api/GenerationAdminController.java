package com.mediaservice.generation.api;

import com.mediaservice.generation.infrastructure.AdmissionControlAdminService;
import com.mediaservice.generation.infrastructure.SimulatorControlStore;
import java.util.HashMap;
import java.util.Map;
import lombok.Data;
import lombok.RequiredArgsConstructor;
import org.springframework.http.ResponseEntity;
import org.springframework.security.access.prepost.PreAuthorize;
import org.springframework.web.bind.annotation.DeleteMapping;
import org.springframework.web.bind.annotation.GetMapping;
import org.springframework.web.bind.annotation.PostMapping;
import org.springframework.web.bind.annotation.RequestBody;
import org.springframework.web.bind.annotation.RequestMapping;
import org.springframework.web.bind.annotation.RestController;

/**
 * Admin-only endpoints for runtime generation control (simulator chaos knobs and admission overrides).
 *
 * <p>Method-level authorization via {@link PreAuthorize} requires Spring Security method security
 * to be enabled; see {@link com.mediaservice.shared.auth.SecurityConfig}, which carries
 * {@code @EnableMethodSecurity}.
 */
@RestController
@RequestMapping("/admin/generation")
@RequiredArgsConstructor
@PreAuthorize("hasRole('ADMIN')")
public class GenerationAdminController {

  private final SimulatorControlStore simulatorControlStore;
  private final AdmissionControlAdminService admissionControlAdminService;

  @GetMapping("/simulator-control")
  public ResponseEntity<Map<String, Object>> getSimulatorControl() {
    return simulatorControlStore.get()
        .<ResponseEntity<Map<String, Object>>>map(snapshot -> {
          Map<String, Object> body = new HashMap<>();
          body.put("configured", true);
          body.put("paused", snapshot.paused());
          body.put("mean_duration_ms", snapshot.meanDurationMs());
          body.put("failure_rate", snapshot.failureRate());
          body.put("updated_at", snapshot.updatedAt());
          return ResponseEntity.ok(body);
        })
        .orElseGet(() -> ResponseEntity.ok(Map.of("configured", false)));
  }

  @PostMapping("/simulator-control")
  public ResponseEntity<Map<String, Object>> updateSimulatorControl(@RequestBody SimulatorControlRequest request) {
    simulatorControlStore.update(request.paused, request.meanDurationMs, request.failureRate);
    return ResponseEntity.ok(Map.of("updated", true));
  }

  @DeleteMapping("/simulator-control")
  public ResponseEntity<Map<String, Object>> clearSimulatorControl() {
    simulatorControlStore.clear();
    return ResponseEntity.ok(Map.of("deleted", true));
  }

  @PostMapping("/admission-control")
  public ResponseEntity<Map<String, Object>> updateAdmissionControl(@RequestBody AdmissionControlRequest request) {
    if (request.tier != null && request.paused != null) {
      admissionControlAdminService.setTierPause(request.tier, request.paused);
    }
    if (request.tenantId != null && request.abuse != null) {
      admissionControlAdminService.setTenantAbuse(request.tenantId, request.abuse);
    }
    if (request.tenantId != null && request.balanceUsd != null) {
      admissionControlAdminService.setTenantBalance(request.tenantId, request.balanceUsd);
    }
    return ResponseEntity.ok(Map.of("updated", true));
  }

  /**
   * Cross-region failover control. Only flips a Redis flag; the actual cross-region routing is
   * deployed by operator action via DNS/Route53. Setting state to FAILED_OVER causes the
   * admission layer to reject new submissions with {@code ADMISSION_REGION_FAILED_OVER}.
   */
  @GetMapping("/region-control")
  public ResponseEntity<Map<String, Object>> getRegionControl() {
    var state = admissionControlAdminService.getRegionState();
    return ResponseEntity.ok(Map.of("state", state.state(), "reason", state.reason()));
  }

  @PostMapping("/region-control")
  public ResponseEntity<Map<String, Object>> setRegionControl(@RequestBody RegionControlRequest request) {
    if (request.state == null) {
      return ResponseEntity.ok(Map.of("updated", true));
    }
    String normalized = request.state.toUpperCase().replace('-', '_');
    if (!normalized.equals("PRIMARY_HEALTHY") && !normalized.equals("DEGRADED") && !normalized.equals("FAILED_OVER")) {
      return ResponseEntity.badRequest().body(Map.of(
          "error", "state must be PRIMARY_HEALTHY, DEGRADED, or FAILED_OVER"));
    }
    admissionControlAdminService.setRegionState(normalized, request.reason);
    return ResponseEntity.ok(Map.of("updated", true));
  }

  @Data
  public static class SimulatorControlRequest {
    private Boolean paused;
    private Long meanDurationMs;
    private Double failureRate;
  }

  @Data
  public static class AdmissionControlRequest {
    private String tier;
    private Boolean paused;
    private String tenantId;
    private Boolean abuse;
    private Double balanceUsd;
  }

  @Data
  public static class RegionControlRequest {
    private String state;
    private String reason;
  }
}
