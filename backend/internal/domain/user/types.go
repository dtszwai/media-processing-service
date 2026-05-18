// Package user holds pure domain types for tenant users and API keys. No SDK
// imports — app/auth owns the on-disk shape.
package user

import "time"

// Role mirrors the transport role enum but is kept here so the domain can
// reason about authorization without importing the auth package.
type Role string

const (
	RoleUser  Role = "USER"
	RoleAdmin Role = "ADMIN"
)

// User is a tenant member. PasswordHash is bcrypt-encoded; never plaintext.
type User struct {
	ID           string
	TenantID     string
	Email        string
	PasswordHash []byte
	Roles        []Role
	CreatedAt    time.Time
}

// APIKey is the metadata stored for a single key. The raw key is never
// persisted — only its SHA-256 hash, kept on a separate authentication row.
type APIKey struct {
	ID        string
	TenantID  string
	UserID    string
	KeyHash   string
	Name      string
	CreatedAt time.Time
}
