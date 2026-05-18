package generation

import (
	"context"
	"errors"
	"os"

	connect "connectrpc.com/connect"

	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

func authorizeGenerationRead(claims *jwtauth.Claims, job *domaingen.Job) error {
	if claims == nil || job == nil || claims.TenantID != job.TenantID {
		return connect.NewError(connect.CodePermissionDenied, errors.New("generation access denied"))
	}
	for _, role := range claims.Roles {
		if role == jwtauth.RoleAdmin {
			return nil
		}
	}
	for _, scope := range claims.Scopes {
		if scope == "media:read:any" || scope == "media:read:tenant" || scope == "generation:read:any" {
			return nil
		}
	}
	if job.UserID == "" || job.UserID == claims.Subject {
		return nil
	}
	return connect.NewError(connect.CodePermissionDenied, errors.New("generation access denied"))
}

func localOnlyGenerationContext(ctx context.Context) context.Context {
	if os.Getenv("LOCAL_ONLY") != "true" {
		return ctx
	}
	if _, err := jwtauth.FromContext(ctx); err == nil {
		return ctx
	}
	tenantID := os.Getenv("LOCAL_TENANT_ID")
	if tenantID == "" {
		tenantID = "tenant_local"
	}
	userID := os.Getenv("LOCAL_USER_ID")
	if userID == "" {
		userID = "user_local"
	}
	return jwtauth.WithPrincipal(ctx, jwtauth.Principal{
		TenantID: tenantID,
		UserID:   userID,
		Roles:    []jwtauth.Role{jwtauth.RoleAdmin},
	})
}
