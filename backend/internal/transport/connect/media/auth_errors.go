package media

import (
	"context"
	"errors"

	connect "connectrpc.com/connect"

	analyticsapp "github.com/dtszwai/media-processing-service/backend/internal/app/analytics"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

func principalFromContext(ctx context.Context) (mediaapp.Principal, error) {
	p, err := jwtauth.FromContext(ctx)
	if err != nil || p.TenantID == "" {
		return mediaapp.Principal{}, connect.NewError(connect.CodeUnauthenticated, errors.New("tenant required"))
	}
	roles := make([]string, 0, len(p.Roles))
	for _, role := range p.Roles {
		roles = append(roles, string(role))
	}
	return mediaapp.Principal{
		TenantID: p.TenantID,
		UserID:   p.UserID,
		APIKeyID: p.APIKeyID,
		Roles:    roles,
		Scopes:   append([]string(nil), p.Scopes...),
	}, nil
}

// appError maps an app-layer error to a Connect status code via typed
// sentinels exported from app/media. App-layer wording is not consulted —
// unrecognized errors fall through to CodeInternal so a future error string
// can never silently re-classify a response.
func appError(err error) error {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, mediaapp.ErrForbidden):
		return connect.NewError(connect.CodePermissionDenied, err)
	case errors.Is(err, mediaapp.ErrNoAssetForRole):
		return connect.NewError(connect.CodeNotFound, err)
	case errors.Is(err, mediaapp.ErrIdempotencyKeyReused):
		return connect.NewError(connect.CodeAlreadyExists, err)
	case errors.Is(err, mediaapp.ErrInvalidOperation),
		errors.Is(err, mediaapp.ErrInvalidInput):
		return connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, mediaapp.ErrRetryExhausted),
		errors.Is(err, mediaapp.ErrPreconditionFailed):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

func canAssignOwner(p mediaapp.Principal) bool {
	for _, role := range p.Roles {
		if role == "ADMIN" {
			return true
		}
	}
	for _, scope := range p.Scopes {
		if scope == "media:write:any" || scope == "media:write:tenant" {
			return true
		}
	}
	return false
}

func principalID(p mediaapp.Principal) string {
	if p.UserID != "" {
		return p.UserID
	}
	return p.APIKeyID
}

func (s *Server) emitAnalytics(ctx context.Context, evt analyticsapp.Event) {
	if s.tracker == nil {
		return
	}
	_ = s.tracker.Track(ctx, evt)
}
