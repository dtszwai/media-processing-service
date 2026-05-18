package middleware

import (
	"context"
	"net/http"

	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
	"github.com/dtszwai/media-processing-service/backend/internal/util/randid"
)

type RequestQuotaMeter interface {
	RecordRequest(ctx context.Context, tenantID, userID, apiKeyID, routeClass, reservationID string) error
}

func QuotaMeterMiddleware(meter RequestQuotaMeter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if meter == nil {
				next.ServeHTTP(w, r)
				return
			}
			p, err := jwtauth.FromContext(r.Context())
			if err != nil || p.TenantID == "" {
				next.ServeHTTP(w, r)
				return
			}
			reservationID := randid.New()
			if err := meter.RecordRequest(r.Context(), p.TenantID, p.UserID, p.APIKeyID, classifyRoute(r), reservationID); err != nil {
				http.Error(w, "quota exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
