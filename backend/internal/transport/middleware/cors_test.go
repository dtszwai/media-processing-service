package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSPreflight(t *testing.T) {
	mw := CORSMiddleware(DefaultCORSConfig())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("preflight should short-circuit, but inner handler ran")
	}))

	req := httptest.NewRequest(http.MethodOptions, "/mediaservice.media.v1.MediaService/ListMedia", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	req.Header.Set("Access-Control-Request-Method", "POST")
	req.Header.Set("Access-Control-Request-Headers", "authorization, content-type")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status: got %d want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3001" {
		t.Fatalf("ACAO: got %q want %q", got, "http://localhost:3001")
	}
	if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
		t.Fatalf("ACAC: got %q want true", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("ACAM empty")
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "authorization, content-type" {
		t.Fatalf("ACAH: got %q want echo of request headers", got)
	}
}

func TestCORSActualRequestAllowed(t *testing.T) {
	called := false
	mw := CORSMiddleware(DefaultCORSConfig())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mediaservice.media.v1.MediaService/ListMedia", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("inner handler not called for non-preflight")
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3001" {
		t.Fatalf("ACAO: got %q", got)
	}
	if got := rec.Header().Get("Vary"); got == "" {
		t.Fatal("Vary header missing")
	}
}

func TestCORSDisallowedOrigin(t *testing.T) {
	mw := CORSMiddleware(DefaultCORSConfig())
	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/mediaservice.media.v1.MediaService/ListMedia", nil)
	req.Header.Set("Origin", "http://evil.example")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("ACAO leaked for disallowed origin: %q", got)
	}
}

func TestCORSConfigFromEnv(t *testing.T) {
	cfg := CORSConfigFromEnv("https://app.example.com, https://admin.example.com")
	if len(cfg.AllowedOrigins) != 2 {
		t.Fatalf("origins parsed: got %v", cfg.AllowedOrigins)
	}
	if cfg.AllowedOrigins[0] != "https://app.example.com" || cfg.AllowedOrigins[1] != "https://admin.example.com" {
		t.Fatalf("origins value: %v", cfg.AllowedOrigins)
	}

	def := CORSConfigFromEnv("")
	if len(def.AllowedOrigins) == 0 {
		t.Fatal("empty env should fall back to defaults")
	}
}
