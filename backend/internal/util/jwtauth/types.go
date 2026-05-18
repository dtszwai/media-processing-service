// Package jwtauth defines principal/tenant types and JWT helpers used by the
// transport-layer middleware.
package jwtauth

import (
	"context"
	"errors"
)

type Role string

const (
	RoleUser  Role = "USER"
	RoleAdmin Role = "ADMIN"
)

type TokenType string

const (
	TokenTypeAccess  TokenType = "access"
	TokenTypeRefresh TokenType = "refresh"
)

const ScopePlatformAdmin = "platform:admin"

// Principal is the authenticated caller derived from a JWT or API key.
type Principal struct {
	TenantID string
	UserID   string
	APIKeyID string
	Roles    []Role
	Scopes   []string
}

// HasRole reports whether the principal carries the given role.
func (p Principal) HasRole(r Role) bool {
	for _, x := range p.Roles {
		if x == r {
			return true
		}
	}
	return false
}

func (p Principal) HasScope(scope string) bool {
	for _, s := range p.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (p Principal) HasPlatformAdmin() bool {
	return p.HasScope(ScopePlatformAdmin)
}

type principalKey struct{}

// WithPrincipal attaches the principal to the context.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, principalKey{}, p)
}

// FromContext returns the principal stored on the context, or ErrUnauthenticated.
func FromContext(ctx context.Context) (Principal, error) {
	if p, ok := ctx.Value(principalKey{}).(Principal); ok {
		return p, nil
	}
	return Principal{}, ErrUnauthenticated
}

var (
	ErrUnauthenticated = errors.New("auth: unauthenticated")
	ErrUnauthorized    = errors.New("auth: unauthorized")
	ErrCrossTenant     = errors.New("auth: cross-tenant denied")
)
