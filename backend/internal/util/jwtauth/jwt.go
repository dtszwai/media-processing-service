package jwtauth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Package-level sentinels so callers don't have to import jwt/v5 to classify
// errors from VerifyHS256.
var (
	ErrTokenExpired   = errors.New("auth: token expired")
	ErrTokenInvalid   = errors.New("auth: token invalid")
	ErrTokenWrongType = errors.New("auth: token wrong type")
	ErrWeakSecret     = errors.New("auth: weak jwt secret")
)

// Claims is the JWT payload the service understands. Implements jwt.Claims for
// jwt/v5 so exp/iat are checked automatically during ParseWithClaims.
type Claims struct {
	Subject   string    `json:"sub"`
	TenantID  string    `json:"tenant_id"`
	TokenType TokenType `json:"token_type"`
	Roles     []Role    `json:"roles,omitempty"`
	Scopes    []string  `json:"scopes,omitempty"`
	IssuedAt  int64     `json:"iat"`
	Expiry    int64     `json:"exp"`
}

func (c Claims) GetExpirationTime() (*jwt.NumericDate, error) {
	if c.Expiry == 0 {
		return nil, nil
	}
	return jwt.NewNumericDate(time.Unix(c.Expiry, 0)), nil
}

func (c Claims) GetIssuedAt() (*jwt.NumericDate, error) {
	if c.IssuedAt == 0 {
		return nil, nil
	}
	return jwt.NewNumericDate(time.Unix(c.IssuedAt, 0)), nil
}

func (c Claims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }
func (c Claims) GetIssuer() (string, error)              { return "", nil }
func (c Claims) GetSubject() (string, error)             { return c.Subject, nil }
func (c Claims) GetAudience() (jwt.ClaimStrings, error)  { return nil, nil }

func (c Claims) HasRole(role Role) bool {
	for _, r := range c.Roles {
		if r == role {
			return true
		}
	}
	return false
}

func (c Claims) HasScope(scope string) bool {
	for _, s := range c.Scopes {
		if s == scope {
			return true
		}
	}
	return false
}

func (c Claims) HasPlatformAdmin() bool {
	return c.HasScope(ScopePlatformAdmin)
}

func SignHS256(secret []byte, c Claims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(secret)
}

// VerifyHS256 parses and verifies an HS256 JWT. Errors are translated into
// package sentinels (ErrTokenExpired, ErrTokenInvalid) — callers should not
// need to import jwt/v5.
func VerifyHS256(secret []byte, token string) (*Claims, error) {
	var c Claims
	parsed, err := jwt.ParseWithClaims(token, &c, func(_ *jwt.Token) (any, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, ErrTokenExpired
		}
		return nil, errors.Join(ErrTokenInvalid, err)
	}
	if !parsed.Valid {
		return nil, ErrTokenInvalid
	}
	return &c, nil
}

func VerifyAccessHS256(secret []byte, token string) (*Claims, error) {
	return verifyHS256Type(secret, token, TokenTypeAccess)
}

func VerifyRefreshHS256(secret []byte, token string) (*Claims, error) {
	return verifyHS256Type(secret, token, TokenTypeRefresh)
}

func verifyHS256Type(secret []byte, token string, tokenType TokenType) (*Claims, error) {
	c, err := VerifyHS256(secret, token)
	if err != nil {
		return nil, err
	}
	if c.TokenType != tokenType {
		return nil, ErrTokenWrongType
	}
	return c, nil
}

func ValidateHS256Secret(secret []byte) error {
	if len(secret) < 32 {
		return ErrWeakSecret
	}
	return nil
}
