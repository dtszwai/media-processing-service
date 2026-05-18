package middleware_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/transport/middleware"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

func TestAuthMiddleware_PassesValidJWT(t *testing.T) {
	secret := []byte("test")
	tok, _ := jwtauth.SignHS256(secret, jwtauth.Claims{Subject: "u", TenantID: "t-A", TokenType: jwtauth.TokenTypeAccess, Expiry: time.Now().Add(time.Hour).Unix()})
	var seen jwtauth.Principal
	h := middleware.AuthMiddleware(middleware.AuthConfig{JWTSecret: secret, Enforcement: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen, _ = jwtauth.FromContext(r.Context())
			w.WriteHeader(204)
		}),
	)
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 204 {
		t.Fatalf("status %d", rr.Code)
	}
	if seen.TenantID != "t-A" {
		t.Fatalf("principal tenant = %q", seen.TenantID)
	}
}

func TestAuthMiddleware_RejectsRefreshTokenAsBearer(t *testing.T) {
	secret := []byte("test")
	tok, _ := jwtauth.SignHS256(secret, jwtauth.Claims{Subject: "u", TenantID: "t-A", TokenType: jwtauth.TokenTypeRefresh, Expiry: time.Now().Add(time.Hour).Unix()})
	h := middleware.AuthMiddleware(middleware.AuthConfig{JWTSecret: secret, Enforcement: true})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }),
	)
	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("refresh bearer status = %d, want 401", rr.Code)
	}
}

func TestAuthMiddleware_Reject401_OnMissingCreds(t *testing.T) {
	h := middleware.AuthMiddleware(middleware.AuthConfig{JWTSecret: []byte("k"), Enforcement: true})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }),
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Code != 401 {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestAuthMiddleware_AllowsOpenLocal(t *testing.T) {
	h := middleware.AuthMiddleware(middleware.AuthConfig{JWTSecret: []byte("k"), Enforcement: false})(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }),
	)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
	if rr.Code != 204 {
		t.Fatalf("local mode should allow: %d", rr.Code)
	}
}

// stubAPIKeyAuth is a deterministic APIKeyAuthenticator for tests. Anything
// matching wantKey produces principal; everything else fails.
type stubAPIKeyAuth struct {
	wantKey   string
	principal jwtauth.Principal
}

func (s stubAPIKeyAuth) Authenticate(_ context.Context, raw string) (jwtauth.Principal, error) {
	if raw == s.wantKey {
		return s.principal, nil
	}
	return jwtauth.Principal{}, errors.New("unknown key")
}

func TestAuthMiddleware_AcceptsAPIKey(t *testing.T) {
	stub := stubAPIKeyAuth{
		wantKey: "mps_secret",
		principal: jwtauth.Principal{
			TenantID: "t-A",
			UserID:   "u-1",
			APIKeyID: "k-1",
			Roles:    []jwtauth.Role{jwtauth.RoleUser},
		},
	}
	var seen jwtauth.Principal
	h := middleware.AuthMiddleware(middleware.AuthConfig{
		JWTSecret:   []byte("unused"),
		APIKeyAuth:  stub,
		Enforcement: true,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen, _ = jwtauth.FromContext(r.Context())
		w.WriteHeader(204)
	}))

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-API-Key", "mps_secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 204 {
		t.Fatalf("status %d body=%q", rr.Code, rr.Body.String())
	}
	if seen.TenantID != "t-A" || seen.APIKeyID != "k-1" {
		t.Fatalf("principal = %+v; want tenant t-A api-key k-1", seen)
	}
}

func TestAuthMiddleware_RejectsBadAPIKey(t *testing.T) {
	stub := stubAPIKeyAuth{wantKey: "mps_secret"}
	h := middleware.AuthMiddleware(middleware.AuthConfig{
		JWTSecret:   []byte("k"),
		APIKeyAuth:  stub,
		Enforcement: true,
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))

	req := httptest.NewRequest("GET", "/x", nil)
	req.Header.Set("X-API-Key", "wrong")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 401 {
		t.Fatalf("expected 401, got %d", rr.Code)
	}
}

func TestTenantScopeFromPath_RejectsMismatch(t *testing.T) {
	// Inner records whether it ran; we expect it never to run on a mismatch.
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) })
	h := middleware.TenantScopeFromPath(inner)

	req := httptest.NewRequest("GET", "/v1/tenants/t-VICTIM/media", nil)
	req = req.WithContext(jwtauth.WithPrincipal(req.Context(), jwtauth.Principal{TenantID: "t-ATTACKER"}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d body=%q", rr.Code, rr.Body.String())
	}
}

func TestTenantScopeFromPath_AllowsMatch(t *testing.T) {
	called := false
	h := middleware.TenantScopeFromPath(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(204)
	}))

	req := httptest.NewRequest("GET", "/v1/tenants/t-A/media", nil)
	req = req.WithContext(jwtauth.WithPrincipal(req.Context(), jwtauth.Principal{TenantID: "t-A"}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != 204 || !called {
		t.Fatalf("matched tenant should pass: code=%d called=%v", rr.Code, called)
	}
}

func TestTenantScopeFromPath_PassesWhenUnauthenticated(t *testing.T) {
	// No principal on context → AuthMiddleware (run earlier in the real chain)
	// already decided. TenantScopeFromPath must not double-reject.
	called := false
	h := middleware.TenantScopeFromPath(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(204)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/v1/tenants/t-A/media", nil))
	if !called {
		t.Fatalf("anonymous traffic should fall through to next, did not")
	}
}

func TestTenantScopeFromPath_DoesNotParseMultipartBody(t *testing.T) {
	const body = "--x\r\nContent-Disposition: form-data; name=\"tenant_id\"\r\n\r\nt-B\r\n--x--\r\n"
	called := false
	h := middleware.TenantScopeFromPath(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		if r.MultipartForm != nil {
			t.Fatalf("tenant scope parsed multipart body")
		}
		got, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read body: %v", err)
		}
		if string(got) != body {
			t.Fatalf("body = %q, want original multipart body", got)
		}
		w.WriteHeader(204)
	}))

	req := httptest.NewRequest("POST", "/v1/media", strings.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	req = req.WithContext(jwtauth.WithPrincipal(req.Context(), jwtauth.Principal{TenantID: "t-A"}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != 204 || !called {
		t.Fatalf("multipart request should pass untouched: code=%d called=%v", rr.Code, called)
	}
}

func TestRateLimit_Throttles(t *testing.T) {
	h := middleware.RateLimitMiddleware(middleware.RateLimitOptions{
		RequestsPerMinute: 60,
		Burst:             3,
		KeyFunc:           func(*http.Request) []string { return []string{"k"} },
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))
	ok := 0
	limited := 0
	for i := 0; i < 10; i++ {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest("GET", "/x", nil))
		switch rr.Code {
		case 204:
			ok++
		case 429:
			limited++
		default:
			t.Fatalf("unexpected status %d on i=%d", rr.Code, i)
		}
	}
	if ok < 1 || limited < 1 {
		t.Fatalf("expected both allows and 429s; ok=%d limited=%d", ok, limited)
	}
}

type recordingQuotaMeter struct {
	reservationIDs []string
}

func (m *recordingQuotaMeter) RecordRequest(_ context.Context, _, _, _, _, reservationID string) error {
	m.reservationIDs = append(m.reservationIDs, reservationID)
	return nil
}

func TestQuotaMeter_IgnoresClientRequestIDForReservation(t *testing.T) {
	meter := &recordingQuotaMeter{}
	h := middleware.QuotaMeterMiddleware(meter)(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }),
	)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/x", nil)
		req.Header.Set("X-Request-Id", "attacker-controlled")
		req = req.WithContext(jwtauth.WithPrincipal(req.Context(), jwtauth.Principal{TenantID: "t-A", UserID: "u-1"}))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != 204 {
			t.Fatalf("request %d status = %d", i, rr.Code)
		}
	}
	if len(meter.reservationIDs) != 2 {
		t.Fatalf("reservation ids = %#v", meter.reservationIDs)
	}
	if meter.reservationIDs[0] == "attacker-controlled" || meter.reservationIDs[1] == "attacker-controlled" {
		t.Fatalf("quota used client request id: %#v", meter.reservationIDs)
	}
	if meter.reservationIDs[0] == meter.reservationIDs[1] {
		t.Fatalf("quota reused reservation id for distinct requests: %#v", meter.reservationIDs)
	}
}

func TestRateLimit_BucketKeysIncludeTenantAndPrincipal(t *testing.T) {
	req := httptest.NewRequest("POST", "/mediaservice.generation.v1.GenerationService/CreateGeneration", nil)
	req = req.WithContext(jwtauth.WithPrincipal(req.Context(), jwtauth.Principal{TenantID: "t-A", UserID: "u-1"}))
	keys := middleware.BucketKeys(req)
	wantTenant := "tenant:t-A:route:GENERATION_SUBMIT"
	wantUser := "tenant:t-A:user:u-1:route:GENERATION_SUBMIT"
	if len(keys) != 2 || keys[0] != wantTenant || keys[1] != wantUser {
		t.Fatalf("keys = %#v, want [%q %q]", keys, wantTenant, wantUser)
	}
}

// Two requests from the SAME tenant via DIFFERENT source IPs must share one
// bucket; otherwise an attacker could dodge the limit by rotating IPs.
func TestRateLimit_AuthenticatedKeyIsTenantNotIP(t *testing.T) {
	rlm := middleware.RateLimitMiddleware(middleware.RateLimitOptions{
		RequestsPerMinute: 60,
		Burst:             3,
		// Default BucketKeys: authenticated → tenant|api_key|class
	})
	h := rlm(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))

	send := func(remote string) int {
		req := httptest.NewRequest("POST", "/mediaservice.generation.v1.GenerationService/GetGeneration", nil)
		req.RemoteAddr = remote
		req = req.WithContext(jwtauth.WithPrincipal(req.Context(),
			jwtauth.Principal{TenantID: "t-A", APIKeyID: "k-1"}))
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}

	// Burst exactly 3 → 4th from a fresh IP must still be 429 because the
	// bucket is keyed on tenant+api-key+class, not IP.
	for i := 0; i < 3; i++ {
		if got := send("10.0.0.1:1234"); got != 204 {
			t.Fatalf("warmup i=%d: got %d, want 204", i, got)
		}
	}
	if got := send("10.0.0.2:5678"); got != 429 {
		t.Fatalf("rotated IP: got %d, want 429 (bucket must be tenant-scoped)", got)
	}
}

// Conversely, unauthenticated traffic from two distinct IPs must NOT share a
// bucket; otherwise one noisy guest starves every other guest.
func TestRateLimit_UnauthenticatedKeyIsPerIP(t *testing.T) {
	rlm := middleware.RateLimitMiddleware(middleware.RateLimitOptions{
		RequestsPerMinute: 60,
		Burst:             2,
	})
	h := rlm(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(204) }))

	send := func(remote string) int {
		req := httptest.NewRequest("POST", "/mediaservice.auth.v1.AuthService/Login", nil)
		req.RemoteAddr = remote
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		return rr.Code
	}

	// Exhaust IP A's burst.
	for i := 0; i < 2; i++ {
		if got := send("10.0.0.1:1111"); got != 204 {
			t.Fatalf("IP A warmup i=%d: got %d", i, got)
		}
	}
	if got := send("10.0.0.1:1111"); got != 429 {
		t.Fatalf("IP A overflow: got %d, want 429", got)
	}
	// IP B has its own bucket — must still be allowed.
	if got := send("10.0.0.2:2222"); got != 204 {
		t.Fatalf("IP B fresh: got %d, want 204 (per-IP bucketing broken)", got)
	}
}
