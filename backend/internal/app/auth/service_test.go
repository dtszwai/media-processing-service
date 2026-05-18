package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/user"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

type fakeUsers struct {
	byID map[string]user.User
}

func (f *fakeUsers) Create(_ context.Context, u user.User) error {
	if f.byID == nil {
		f.byID = map[string]user.User{}
	}
	f.byID[u.ID] = u
	return nil
}

func (f *fakeUsers) GetByEmail(context.Context, string) (*user.User, error) {
	return nil, ErrNotFound
}

func (f *fakeUsers) GetByID(_ context.Context, userID string) (*user.User, error) {
	u, ok := f.byID[userID]
	if !ok {
		return nil, ErrNotFound
	}
	return &u, nil
}

func TestRefreshRequiresRefreshToken(t *testing.T) {
	now := time.Now().UTC()
	secret := []byte("0123456789abcdef0123456789abcdef")
	users := &fakeUsers{byID: map[string]user.User{
		"user-1": {
			ID:       "user-1",
			TenantID: "tenant-1",
			Email:    "admin@example.com",
			Roles:    []user.Role{user.RoleAdmin},
		},
	}}
	svc := NewService(users, nil, Config{
		JWTSecret: secret,
		TokenTTL:  15 * time.Minute,
		Now:       func() time.Time { return now },
	})

	access, err := jwtauth.SignHS256(secret, jwtauth.Claims{
		Subject:   "user-1",
		TenantID:  "tenant-1",
		TokenType: jwtauth.TokenTypeAccess,
		Expiry:    time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign access: %v", err)
	}
	if _, err := svc.Refresh(context.Background(), access); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("Refresh(access) err = %v, want ErrInvalidCredentials", err)
	}

	refresh, err := jwtauth.SignHS256(secret, jwtauth.Claims{
		Subject:   "user-1",
		TenantID:  "tenant-1",
		TokenType: jwtauth.TokenTypeRefresh,
		Expiry:    time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign refresh: %v", err)
	}
	sess, err := svc.Refresh(context.Background(), refresh)
	if err != nil {
		t.Fatalf("Refresh(refresh): %v", err)
	}
	if _, err := jwtauth.VerifyAccessHS256(secret, sess.AccessToken); err != nil {
		t.Fatalf("new access token type: %v", err)
	}
	if _, err := jwtauth.VerifyRefreshHS256(secret, sess.RefreshToken); err != nil {
		t.Fatalf("new refresh token type: %v", err)
	}
}

func TestMintSessionDoesNotGrantPlatformAdminToTenantAdmin(t *testing.T) {
	now := time.Now().UTC()
	secret := []byte("0123456789abcdef0123456789abcdef")
	svc := NewService(nil, nil, Config{
		JWTSecret: secret,
		TokenTTL:  15 * time.Minute,
		Now:       func() time.Time { return now },
	})

	sess, err := svc.mintSession("user-1", "tenant-1", []user.Role{user.RoleAdmin}, now)
	if err != nil {
		t.Fatalf("mintSession: %v", err)
	}
	claims, err := jwtauth.VerifyAccessHS256(secret, sess.AccessToken)
	if err != nil {
		t.Fatalf("verify access: %v", err)
	}
	if !claims.HasRole(jwtauth.RoleAdmin) {
		t.Fatalf("tenant admin role missing: %+v", claims)
	}
	if claims.HasPlatformAdmin() {
		t.Fatalf("tenant admin session includes platform admin scope: %+v", claims)
	}
}
