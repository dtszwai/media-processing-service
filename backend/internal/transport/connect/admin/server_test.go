package admin

import (
	"context"
	"testing"

	connect "connectrpc.com/connect"

	"github.com/dtszwai/media-processing-service/backend/internal/transport/connect/authz"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

func TestAdminAuthTenantAdminIsNotPlatformOperator(t *testing.T) {
	ctx := jwtauth.WithPrincipal(context.Background(), jwtauth.Principal{
		UserID:   "user-1",
		TenantID: "tenant-1",
		Roles:    []jwtauth.Role{jwtauth.RoleAdmin},
	})

	if _, err := authz.RequireAdmin(ctx); err != nil {
		t.Fatalf("tenant admin should pass tenant admin gate: %v", err)
	}
	if _, err := authz.RequirePlatformAdmin(ctx); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("tenant admin platform gate err = %v, want PermissionDenied", err)
	}
}

func TestAdminAuthPlatformScopeIsDistinctGrant(t *testing.T) {
	ctx := jwtauth.WithPrincipal(context.Background(), jwtauth.Principal{
		UserID:   "operator-1",
		TenantID: "platform",
		Scopes:   []string{jwtauth.ScopePlatformAdmin},
	})

	if _, err := authz.RequirePlatformAdmin(ctx); err != nil {
		t.Fatalf("platform scope should pass platform gate: %v", err)
	}
	if _, err := authz.RequireAdmin(ctx); err != nil {
		t.Fatalf("platform scope should pass admin gate: %v", err)
	}
}

func TestAdminAuthRejectsMissingPrincipal(t *testing.T) {
	if _, err := authz.RequirePlatformAdmin(context.Background()); connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("missing principal err = %v, want Unauthenticated", err)
	}
}

func TestTenantIDForAdminRequestScopesTenantAdmins(t *testing.T) {
	tenantAdmin := &jwtauth.Claims{TenantID: "tenant-1", Roles: []jwtauth.Role{jwtauth.RoleAdmin}}
	if got, err := tenantIDForAdminRequest(tenantAdmin, "tenant-1", true); err != nil || got != "tenant-1" {
		t.Fatalf("own tenant got=%q err=%v, want tenant-1 nil", got, err)
	}
	if _, err := tenantIDForAdminRequest(tenantAdmin, "tenant-2", true); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("cross-tenant err = %v, want PermissionDenied", err)
	}

	platform := &jwtauth.Claims{TenantID: "platform", Scopes: []string{jwtauth.ScopePlatformAdmin}}
	if got, err := tenantIDForAdminRequest(platform, "tenant-2", true); err != nil || got != "tenant-2" {
		t.Fatalf("platform cross-tenant got=%q err=%v, want tenant-2 nil", got, err)
	}
}
