package auth

import (
	"context"
	"testing"
	"time"

	connect "connectrpc.com/connect"

	authapp "github.com/dtszwai/media-processing-service/backend/internal/app/auth"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/user"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
	authpb "github.com/dtszwai/media-processing-service/backend/pkg/contracts/mediaservice/auth/v1"
)

type fakeUsers struct{}

func (fakeUsers) Create(context.Context, user.User) error { return nil }
func (fakeUsers) GetByEmail(context.Context, string) (*user.User, error) {
	return nil, authapp.ErrNotFound
}
func (fakeUsers) GetByID(context.Context, string) (*user.User, error) {
	return nil, authapp.ErrNotFound
}

type fakeAPIKeys struct {
	created   int
	listed    int
	deleted   int
	deletedBy string
}

func (f *fakeAPIKeys) Create(_ context.Context, k user.APIKey, _ string) error {
	f.created++
	return nil
}

func (f *fakeAPIKeys) ListByTenant(context.Context, string) ([]user.APIKey, error) {
	f.listed++
	return []user.APIKey{{ID: "key-1", Name: "primary", CreatedAt: time.Unix(100, 0).UTC()}}, nil
}

func (f *fakeAPIKeys) Delete(_ context.Context, _, keyID string) error {
	f.deleted++
	f.deletedBy = keyID
	return nil
}

func newTestServer(keys *fakeAPIKeys) *Server {
	svc := authapp.NewService(fakeUsers{}, keys, authapp.Config{
		JWTSecret: []byte("0123456789abcdef0123456789abcdef"),
		IDGen:     func() string { return "id-1" },
		Now:       func() time.Time { return time.Unix(100, 0).UTC() },
	})
	return NewServer(svc)
}

func principalCtx(p jwtauth.Principal) context.Context {
	return jwtauth.WithPrincipal(context.Background(), p)
}

func TestCreateAPIKeyRejectsAPIKeyPrincipal(t *testing.T) {
	keys := &fakeAPIKeys{}
	server := newTestServer(keys)
	ctx := principalCtx(jwtauth.Principal{
		TenantID: "tenant-1",
		UserID:   "user-1",
		APIKeyID: "key-existing",
		Scopes:   []string{scopeAPIKeysManageTenant},
	})

	_, err := server.CreateAPIKey(ctx, connect.NewRequest(&authpb.CreateAPIKeyRequest{Name: "next"}))
	if connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("CreateAPIKey err = %v, want PermissionDenied", err)
	}
	if keys.created != 0 {
		t.Fatalf("CreateAPIKey reached service for API-key principal")
	}
}

func TestAPIKeyManagementRequiresAdminOrTenantScope(t *testing.T) {
	keys := &fakeAPIKeys{}
	server := newTestServer(keys)
	userCtx := principalCtx(jwtauth.Principal{
		TenantID: "tenant-1",
		UserID:   "user-1",
		Roles:    []jwtauth.Role{jwtauth.RoleUser},
	})

	if _, err := server.ListAPIKeys(userCtx, connect.NewRequest(&authpb.ListAPIKeysRequest{})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("ListAPIKeys user err = %v, want PermissionDenied", err)
	}
	if _, err := server.DeleteAPIKey(userCtx, connect.NewRequest(&authpb.DeleteAPIKeyRequest{KeyId: "key-1"})); connect.CodeOf(err) != connect.CodePermissionDenied {
		t.Fatalf("DeleteAPIKey user err = %v, want PermissionDenied", err)
	}

	adminCtx := principalCtx(jwtauth.Principal{
		TenantID: "tenant-1",
		UserID:   "admin-1",
		Roles:    []jwtauth.Role{jwtauth.RoleAdmin},
	})
	if _, err := server.CreateAPIKey(adminCtx, connect.NewRequest(&authpb.CreateAPIKeyRequest{Name: "primary"})); err != nil {
		t.Fatalf("CreateAPIKey admin: %v", err)
	}

	scopedCtx := principalCtx(jwtauth.Principal{
		TenantID: "tenant-1",
		UserID:   "ops-1",
		Scopes:   []string{scopeAPIKeysManageTenant},
	})
	if _, err := server.ListAPIKeys(scopedCtx, connect.NewRequest(&authpb.ListAPIKeysRequest{})); err != nil {
		t.Fatalf("ListAPIKeys scoped: %v", err)
	}
	if _, err := server.DeleteAPIKey(scopedCtx, connect.NewRequest(&authpb.DeleteAPIKeyRequest{KeyId: "key-1"})); err != nil {
		t.Fatalf("DeleteAPIKey scoped: %v", err)
	}

	if keys.created != 1 || keys.listed != 1 || keys.deleted != 1 || keys.deletedBy != "key-1" {
		t.Fatalf("service calls = created:%d listed:%d deleted:%d key:%q", keys.created, keys.listed, keys.deleted, keys.deletedBy)
	}
}
