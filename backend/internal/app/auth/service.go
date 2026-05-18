// Package auth implements user / API-key lifecycle, JWT-backed
// authentication, and the principal types the transport middleware threads
// onto the request context.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	auditapp "github.com/dtszwai/media-processing-service/backend/internal/app/audit"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/user"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

// UserRepository abstracts the user persistence layer so the service stays
// independent of DDB. Implemented by DDBUsers.
type UserRepository interface {
	Create(ctx context.Context, u user.User) error
	GetByEmail(ctx context.Context, email string) (*user.User, error)
	GetByID(ctx context.Context, userID string) (*user.User, error)
}

// APIKeyRepository abstracts the API-key persistence layer.
type APIKeyRepository interface {
	Create(ctx context.Context, k user.APIKey, raw string) error
	ListByTenant(ctx context.Context, tenantID string) ([]user.APIKey, error)
	Delete(ctx context.Context, tenantID, keyID string) error
}

// ErrInvalidCredentials signals a missing user or bad password to the
// transport layer without leaking which case it was.
var ErrInvalidCredentials = errors.New("auth: invalid credentials")

// ErrPasswordTooShort is returned by Register when the password fails the
// length floor.
var ErrPasswordTooShort = errors.New("auth: password too short")

// Session is the post-authentication payload: identifiers + freshly minted
// JWTs. Returned by Register and Login.
type Session struct {
	UserID       string
	TenantID     string
	AccessToken  string
	RefreshToken string
	ExpiresIn    int32 // seconds until access token expiry
}

// Service is the pure auth application service. It contains no transport
// concerns; Connect mapping lives in internal/transport/connect/auth.
type Service struct {
	users      UserRepository
	keys       APIKeyRepository
	jwtSecret  []byte
	tokenTTL   time.Duration
	idGen      func() string
	now        func() time.Time
	bcryptCost int
	// recorder receives per-event audit rows for login + API-key events.
	// Nil-safe via NoopRecorder default so test callers don't need to wire
	// it; production paths inject the DDB-backed Recorder.
	recorder auditapp.Recorder
}

// Config carries non-secret tunables. JWTSecret must come from env or KMS.
type Config struct {
	JWTSecret  []byte
	TokenTTL   time.Duration
	IDGen      func() string
	Now        func() time.Time
	BcryptCost int
	// Recorder is the standalone audit Recorder. Optional: when nil the
	// service substitutes a NoopRecorder so call sites stay branch-free.
	Recorder auditapp.Recorder
}

// NewService builds the auth service with sensible defaults.
func NewService(users UserRepository, keys APIKeyRepository, cfg Config) *Service {
	s := &Service{
		users:      users,
		keys:       keys,
		jwtSecret:  cfg.JWTSecret,
		tokenTTL:   cfg.TokenTTL,
		idGen:      cfg.IDGen,
		now:        cfg.Now,
		bcryptCost: cfg.BcryptCost,
		recorder:   cfg.Recorder,
	}
	if s.tokenTTL == 0 {
		s.tokenTTL = time.Hour
	}
	if s.idGen == nil {
		s.idGen = defaultID
	}
	if s.now == nil {
		s.now = time.Now
	}
	if s.bcryptCost == 0 {
		s.bcryptCost = bcrypt.DefaultCost
	}
	if s.recorder == nil {
		s.recorder = auditapp.NoopRecorder{}
	}
	return s
}

// Register creates a tenant + admin user atomically and mints a session.
func (s *Service) Register(ctx context.Context, email, password string) (Session, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		return Session{}, ErrInvalidCredentials
	}
	if len(password) < 8 {
		return Session{}, ErrPasswordTooShort
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), s.bcryptCost)
	if err != nil {
		return Session{}, err
	}
	tenantID := s.idGen()
	userID := s.idGen()
	now := s.now().UTC()
	u := user.User{
		ID:           userID,
		TenantID:     tenantID,
		Email:        email,
		PasswordHash: hash,
		Roles:        []user.Role{user.RoleAdmin},
		CreatedAt:    now,
	}
	if err := s.users.Create(ctx, u); err != nil {
		return Session{}, err
	}
	return s.mintSession(userID, tenantID, u.Roles, now)
}

// Refresh verifies the refresh token, re-resolves the user, and mints a fresh
// session. ErrInvalidCredentials is returned when the token fails verification
// or the user no longer exists.
func (s *Service) Refresh(ctx context.Context, refreshToken string) (Session, error) {
	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return Session{}, ErrInvalidCredentials
	}
	claims, err := jwtauth.VerifyRefreshHS256(s.jwtSecret, refreshToken)
	if err != nil {
		return Session{}, ErrInvalidCredentials
	}
	u, err := s.users.GetByID(ctx, claims.Subject)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, err
	}
	return s.mintSession(u.ID, u.TenantID, u.Roles, s.now().UTC())
}

// Login verifies credentials and mints a session.
func (s *Service) Login(ctx context.Context, email, password string) (Session, error) {
	email = strings.TrimSpace(strings.ToLower(email))
	if email == "" || password == "" {
		s.recordLoginFailure(ctx, email, "MISSING_CREDENTIALS")
		return Session{}, ErrInvalidCredentials
	}
	u, err := s.users.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			s.recordLoginFailure(ctx, email, "UNKNOWN_USER")
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, err
	}
	if err := bcrypt.CompareHashAndPassword(u.PasswordHash, []byte(password)); err != nil {
		s.recordLoginFailure(ctx, email, "BAD_PASSWORD")
		return Session{}, ErrInvalidCredentials
	}
	sess, err := s.mintSession(u.ID, u.TenantID, u.Roles, s.now().UTC())
	if err != nil {
		return sess, err
	}
	s.recordLoginSuccess(ctx, u.ID, u.TenantID)
	return sess, nil
}

// recordLoginSuccess emits an identity.login.succeeded audit. Errors are
// swallowed: a failed audit must not deny the user a session that already
// passed credential checks. Audit reliability is observed via the
// Recorder's own metrics, not via the auth path.
func (s *Service) recordLoginSuccess(ctx context.Context, userID, tenantID string) {
	_ = s.recorder.Record(ctx, auditapp.NewIdentityLoginSucceeded(userID, tenantID, ""))
}

// recordLoginFailure emits an identity.login.failed audit. Operators
// monitor this stream for brute-force patterns, so the actor id captures
// the submitted email even when no user resolves. Empty-email failures
// (input-validation rejections before a lookup) skip the audit row —
// there's nothing to thread on the actor GSI and the row would be visible
// noise on dashboards.
func (s *Service) recordLoginFailure(ctx context.Context, email, reasonCode string) {
	if email == "" {
		return
	}
	_ = s.recorder.Record(ctx, auditapp.NewIdentityLoginFailed(email, reasonCode, ""))
}

// GetUser returns the user profile.
func (s *Service) GetUser(ctx context.Context, userID string) (*user.User, error) {
	return s.users.GetByID(ctx, userID)
}

// CreateAPIKey issues a new key. The raw key is returned exactly once.
func (s *Service) CreateAPIKey(ctx context.Context, tenantID, userID, name string) (user.APIKey, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return user.APIKey{}, "", errors.New("auth: name required")
	}
	raw, err := generateRawAPIKey()
	if err != nil {
		return user.APIKey{}, "", err
	}
	rec := user.APIKey{
		ID:        s.idGen(),
		TenantID:  tenantID,
		UserID:    userID,
		Name:      name,
		CreatedAt: s.now().UTC(),
	}
	if err := s.keys.Create(ctx, rec, raw); err != nil {
		return user.APIKey{}, "", err
	}
	_ = s.recorder.Record(ctx, auditapp.NewAPIKeyCreated(tenantID, userID, rec.ID, name, ""))
	return rec, raw, nil
}

// ListAPIKeys returns the tenant's keys without their raw value.
func (s *Service) ListAPIKeys(ctx context.Context, tenantID string) ([]user.APIKey, error) {
	return s.keys.ListByTenant(ctx, tenantID)
}

// DeleteAPIKeyAsUser revokes a key and emits an audit row attributed to
// the acting user. Transports that have already resolved the JWT pass the
// actor through here so the ACTOR# GSI partition is non-empty.
func (s *Service) DeleteAPIKeyAsUser(ctx context.Context, tenantID, actorUserID, keyID string) error {
	if keyID == "" {
		return errors.New("auth: key_id required")
	}
	if err := s.keys.Delete(ctx, tenantID, keyID); err != nil {
		return err
	}
	_ = s.recorder.Record(ctx, auditapp.NewAPIKeyRevoked(tenantID, actorUserID, keyID, ""))
	return nil
}

// mintSession signs an access JWT and a refresh JWT (longer-lived, same secret
// for now — rotation belongs in a later phase).
func (s *Service) mintSession(userID, tenantID string, roles []user.Role, now time.Time) (Session, error) {
	domainRoles := make([]jwtauth.Role, len(roles))
	for i, r := range roles {
		domainRoles[i] = jwtauth.Role(r)
	}
	access := jwtauth.Claims{
		Subject:   userID,
		TenantID:  tenantID,
		TokenType: jwtauth.TokenTypeAccess,
		Roles:     domainRoles,
		IssuedAt:  now.Unix(),
		Expiry:    now.Add(s.tokenTTL).Unix(),
	}
	token, err := jwtauth.SignHS256(s.jwtSecret, access)
	if err != nil {
		return Session{}, err
	}
	refreshClaims := access
	refreshClaims.TokenType = jwtauth.TokenTypeRefresh
	refreshClaims.Expiry = now.Add(s.tokenTTL * 24 * 7).Unix()
	refresh, err := jwtauth.SignHS256(s.jwtSecret, refreshClaims)
	if err != nil {
		return Session{}, err
	}
	return Session{
		UserID:       userID,
		TenantID:     tenantID,
		AccessToken:  token,
		RefreshToken: refresh,
		ExpiresIn:    int32(s.tokenTTL.Seconds()),
	}, nil
}

// generateRawAPIKey produces a 32-byte url-safe random string prefixed with
// "mps_" so it's instantly recognizable in logs.
func generateRawAPIKey() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "mps_" + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func defaultID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}
