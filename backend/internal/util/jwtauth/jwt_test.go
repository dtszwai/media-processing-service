package jwtauth_test

import (
	"strings"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

func TestJWT_SignAndVerify(t *testing.T) {
	secret := []byte("super-secret")
	c := jwtauth.Claims{
		Subject:   "user-1",
		TenantID:  "tenant-1",
		TokenType: jwtauth.TokenTypeAccess,
		Roles:     []jwtauth.Role{jwtauth.RoleUser},
		IssuedAt:  time.Now().Unix(),
		Expiry:    time.Now().Add(15 * time.Minute).Unix(),
	}
	tok, err := jwtauth.SignHS256(secret, c)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	if !strings.HasPrefix(tok, "eyJ") {
		t.Fatalf("token should start with eyJ: %s", tok)
	}
	got, err := jwtauth.VerifyHS256(secret, tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.TenantID != "tenant-1" || got.Subject != "user-1" || got.TokenType != jwtauth.TokenTypeAccess {
		t.Fatalf("claims drift: %+v", got)
	}
}

func TestJWT_RejectsWrongSecret(t *testing.T) {
	tok, _ := jwtauth.SignHS256([]byte("good"), jwtauth.Claims{Subject: "s", TokenType: jwtauth.TokenTypeAccess, Expiry: time.Now().Add(time.Hour).Unix()})
	if _, err := jwtauth.VerifyHS256([]byte("bad"), tok); err == nil {
		t.Fatalf("expected signature mismatch")
	}
}

func TestJWT_RejectsExpired(t *testing.T) {
	tok, _ := jwtauth.SignHS256([]byte("k"), jwtauth.Claims{Subject: "s", TokenType: jwtauth.TokenTypeAccess, Expiry: time.Now().Add(-time.Minute).Unix()})
	if _, err := jwtauth.VerifyHS256([]byte("k"), tok); err == nil {
		t.Fatalf("expected token-expired error")
	}
}

func TestJWT_AccessAndRefreshTypesAreDistinct(t *testing.T) {
	secret := []byte("0123456789abcdef0123456789abcdef")
	refresh, err := jwtauth.SignHS256(secret, jwtauth.Claims{
		Subject:   "user-1",
		TenantID:  "tenant-1",
		TokenType: jwtauth.TokenTypeRefresh,
		Expiry:    time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("sign refresh: %v", err)
	}
	if _, err := jwtauth.VerifyAccessHS256(secret, refresh); err == nil {
		t.Fatalf("refresh token verified as access token")
	}
	if _, err := jwtauth.VerifyRefreshHS256(secret, refresh); err != nil {
		t.Fatalf("refresh token rejected as refresh token: %v", err)
	}
}

func TestValidateHS256Secret(t *testing.T) {
	if err := jwtauth.ValidateHS256Secret([]byte("0123456789abcdef0123456789abcdef")); err != nil {
		t.Fatalf("strong secret rejected: %v", err)
	}
	if err := jwtauth.ValidateHS256Secret([]byte("too-short")); err == nil {
		t.Fatalf("weak secret accepted")
	}
}
