// Package middleware provides HTTP/Connect middleware for principal context,
// rate limiting, request IDs, and CORS.
package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

// AuthConfig controls JWT / API-key handling.
type AuthConfig struct {
	JWTSecret  []byte
	APIKeyAuth APIKeyAuthenticator
	// Enforcement set to false makes the middleware optional — used in local mode.
	Enforcement bool
	// PublicPaths are URL path prefixes that never require credentials, even
	// when Enforcement is on. Used for register/login endpoints.
	PublicPaths []string
}

// APIKeyAuthenticator looks up an API key and returns the matching principal.
// Implementations must populate Principal.APIKeyID so downstream layers (rate
// limiter, audit) can bucket by key rather than user.
type APIKeyAuthenticator interface {
	Authenticate(ctx context.Context, key string) (jwtauth.Principal, error)
}

// AuthMiddleware attaches a principal to the request context. If Enforcement
// is true, requests without valid credentials get a 401, except for paths in
// cfg.PublicPaths (e.g. register/login).
//
// JWT and API-key credentials both produce a uniform jwtauth.Principal so that
// every middleware downstream (tenant-scope, rate-limit, audit) can treat them
// identically. The choice of credential is invisible to handlers.
func AuthMiddleware(cfg AuthConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := extractPrincipal(r, cfg)
			if err == nil {
				r = r.WithContext(jwtauth.WithPrincipal(r.Context(), principal))
				next.ServeHTTP(w, r)
				return
			}
			if !cfg.Enforcement || isPublicPath(r.URL.Path, cfg.PublicPaths) {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "unauthenticated: "+err.Error(), http.StatusUnauthorized)
		})
	}
}

func isPublicPath(path string, prefixes []string) bool {
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		if path == p || strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

func extractPrincipal(r *http.Request, cfg AuthConfig) (jwtauth.Principal, error) {
	if hdr := r.Header.Get("Authorization"); hdr != "" {
		if tok, ok := strings.CutPrefix(hdr, "Bearer "); ok {
			claims, err := jwtauth.VerifyAccessHS256(cfg.JWTSecret, tok)
			if err != nil {
				return jwtauth.Principal{}, err
			}
			return jwtauth.Principal{
				TenantID: claims.TenantID,
				UserID:   claims.Subject,
				Roles:    claims.Roles,
				Scopes:   claims.Scopes,
			}, nil
		}
	}
	if key := r.Header.Get("X-API-Key"); key != "" && cfg.APIKeyAuth != nil {
		return cfg.APIKeyAuth.Authenticate(r.Context(), key)
	}
	return jwtauth.Principal{}, jwtauth.ErrUnauthenticated
}

// RequireRole gates a handler to principals with the given role.
func RequireRole(role jwtauth.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			p, err := jwtauth.FromContext(r.Context())
			if err != nil {
				http.Error(w, "unauthenticated", http.StatusUnauthorized)
				return
			}
			if !p.HasRole(role) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// TenantScopeFromPath enforces that any tenant identifier carried in request
// metadata matches the authenticated principal's tenant. Requests without a
// principal pass through — AuthMiddleware enforces presence first.
//
// Installed globally rather than per-route so a future route that happens to
// carry a tenant_id can't accidentally skip the cross-tenant check.
func TenantScopeFromPath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := jwtauth.FromContext(r.Context())
		if err != nil {
			next.ServeHTTP(w, r)
			return
		}
		candidates := []string{
			tenantFromPath(r.URL.Path),
			r.Header.Get("X-Tenant-Id"),
			r.URL.Query().Get("tenant_id"),
		}
		for _, c := range candidates {
			if c == "" {
				continue
			}
			if c != p.TenantID {
				http.Error(w, "cross-tenant denied", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// tenantFromPath extracts a tenant id from any path of the shape /.../tenants/{tid}/... so cross-tenant access can be denied before the handler parses its request body.
func tenantFromPath(p string) string {
	i := strings.Index(p, "/tenants/")
	if i < 0 {
		return ""
	}
	rest := p[i+len("/tenants/"):]
	end := strings.IndexByte(rest, '/')
	if end < 0 {
		return rest
	}
	return rest[:end]
}
