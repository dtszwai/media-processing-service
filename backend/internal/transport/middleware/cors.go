package middleware

import (
	"net/http"
	"strings"
	"time"

	"github.com/rs/cors"
)

// CORSConfig controls cross-origin access. AllowedOrigins is a list of exact
// origin strings ("*" allows any). AllowCredentials only takes effect when
// AllowedOrigins does NOT contain "*", per the CORS spec.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	ExposedHeaders   []string
	AllowCredentials bool
	MaxAge           time.Duration
}

func DefaultCORSConfig() CORSConfig {
	return CORSConfig{
		AllowedOrigins: []string{"http://localhost:3001", "http://localhost:5173"},
		AllowedMethods: []string{
			http.MethodGet, http.MethodPost, http.MethodPut,
			http.MethodPatch, http.MethodDelete, http.MethodOptions,
		},
		AllowedHeaders: []string{
			"Authorization", "Content-Type", "X-Api-Key",
			"X-Request-Id", "X-Webhook-Signature", "X-Webhook-Timestamp",
			"Idempotency-Key",
			"Connect-Protocol-Version", "Connect-Timeout-Ms", "Grpc-Timeout",
			"traceparent", "tracestate", "baggage",
		},
		ExposedHeaders:   []string{"X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           10 * time.Minute,
	}
}

// CORSMiddleware sets Access-Control-* headers and short-circuits preflight
// OPTIONS requests with 204. Place outermost so preflights skip auth.
func CORSMiddleware(cfg CORSConfig) func(http.Handler) http.Handler {
	c := cors.New(cors.Options{
		AllowedOrigins:   cfg.AllowedOrigins,
		AllowedMethods:   cfg.AllowedMethods,
		AllowedHeaders:   cfg.AllowedHeaders,
		ExposedHeaders:   cfg.ExposedHeaders,
		AllowCredentials: cfg.AllowCredentials,
		MaxAge:           int(cfg.MaxAge.Seconds()),
	})
	return c.Handler
}

// CORSConfigFromEnv builds CORSConfig from CORS_ALLOWED_ORIGINS (comma-separated).
// Falls back to DefaultCORSConfig when unset.
func CORSConfigFromEnv(originsCSV string) CORSConfig {
	cfg := DefaultCORSConfig()
	originsCSV = strings.TrimSpace(originsCSV)
	if originsCSV == "" {
		return cfg
	}
	parts := strings.Split(originsCSV, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	if len(out) > 0 {
		cfg.AllowedOrigins = out
	}
	return cfg
}
