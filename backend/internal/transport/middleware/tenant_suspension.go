package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

type TenantSuspensionChecker interface {
	IsSuspended(ctx context.Context, tenantID string) (bool, error)
}

func TenantSuspensionMiddleware(checker TenantSuspensionChecker) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if checker == nil || !isTenantMutation(r) || isAdminPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}
			p, err := jwtauth.FromContext(r.Context())
			if err != nil || p.TenantID == "" {
				next.ServeHTTP(w, r)
				return
			}
			suspended, err := checker.IsSuspended(r.Context(), p.TenantID)
			if err != nil {
				http.Error(w, "tenant suspension check failed", http.StatusInternalServerError)
				return
			}
			if suspended {
				http.Error(w, "tenant suspended", http.StatusLocked)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isTenantMutation(r *http.Request) bool {
	switch r.Method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func isAdminPath(path string) bool {
	return path == "/admin" || strings.HasPrefix(path, "/admin/")
}
