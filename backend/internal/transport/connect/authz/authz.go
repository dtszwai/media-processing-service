// Package authz resolves authenticated principals for Connect handlers.
package authz

import (
	"context"
	"errors"

	connect "connectrpc.com/connect"

	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

// Claims extracts the principal installed by HTTP middleware and returns it in
// the older claims shape used by transport authorization code.
func Claims(ctx context.Context) (*jwtauth.Claims, error) {
	p, err := jwtauth.FromContext(ctx)
	if err != nil || p.TenantID == "" {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("unauthenticated"))
	}
	roles := make([]jwtauth.Role, len(p.Roles))
	copy(roles, p.Roles)
	return &jwtauth.Claims{
		TenantID:  p.TenantID,
		Subject:   p.UserID,
		TokenType: jwtauth.TokenTypeAccess,
		Roles:     roles,
		Scopes:    append([]string(nil), p.Scopes...),
	}, nil
}

// RequireAdmin permits tenant admins and platform operators.
func RequireAdmin(ctx context.Context) (*jwtauth.Claims, error) {
	c, err := Claims(ctx)
	if err != nil {
		return nil, err
	}
	if c.HasRole(jwtauth.RoleAdmin) || c.HasPlatformAdmin() {
		return c, nil
	}
	return nil, connect.NewError(connect.CodePermissionDenied, errors.New("admin role required"))
}

// RequirePlatformAdmin permits only callers with the platform admin scope.
func RequirePlatformAdmin(ctx context.Context) (*jwtauth.Claims, error) {
	c, err := Claims(ctx)
	if err != nil {
		return nil, err
	}
	if c.HasPlatformAdmin() {
		return c, nil
	}
	return nil, connect.NewError(connect.CodePermissionDenied, errors.New("platform admin scope required"))
}
