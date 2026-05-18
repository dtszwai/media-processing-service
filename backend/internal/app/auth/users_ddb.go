package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/user"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// DDBUsers persists User profile + email-lookup rows on the single table.
type DDBUsers struct {
	KV kv.KV
}

// NewDDBUsers binds the impl to a kv driver.
func NewDDBUsers(k kv.KV) *DDBUsers { return &DDBUsers{KV: k} }

type userRow struct {
	PK           string    `dynamodbav:"PK"`
	SK           string    `dynamodbav:"SK"`
	ItemType     string    `dynamodbav:"item_type"`
	UserID       string    `dynamodbav:"user_id"`
	TenantID     string    `dynamodbav:"tenant_id"`
	Email        string    `dynamodbav:"email"`
	PasswordHash []byte    `dynamodbav:"password_hash"`
	Roles        []string  `dynamodbav:"roles"`
	CreatedAt    time.Time `dynamodbav:"created_at"`
}

type emailLookupRow struct {
	PK       string `dynamodbav:"PK"`
	SK       string `dynamodbav:"SK"`
	ItemType string `dynamodbav:"item_type"`
	UserID   string `dynamodbav:"user_id"`
	TenantID string `dynamodbav:"tenant_id"`
}

// Create writes profile + email-lookup atomically. ErrEmailTaken on collision.
func (r *DDBUsers) Create(ctx context.Context, u user.User) error {
	if u.ID == "" || u.TenantID == "" || u.Email == "" {
		return errors.New("user: id, tenant_id, email required")
	}
	if u.CreatedAt.IsZero() {
		u.CreatedAt = time.Now().UTC()
	}
	roles := make([]string, len(u.Roles))
	for i, role := range u.Roles {
		roles[i] = string(role)
	}
	profile := userRow{
		PK: UserPK(u.ID), SK: UserProfileSK, ItemType: "USER",
		UserID: u.ID, TenantID: u.TenantID, Email: u.Email,
		PasswordHash: u.PasswordHash, Roles: roles, CreatedAt: u.CreatedAt,
	}
	lookup := emailLookupRow{
		PK: EmailLookupPK(u.Email), SK: EmailLookupSK, ItemType: "EMAIL_LOOKUP",
		UserID: u.ID, TenantID: u.TenantID,
	}
	err := r.KV.TransactWrite(ctx, []kv.WriteOp{
		{Put: &kv.PutOp{
			Item:                profile,
			ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
		}},
		{Put: &kv.PutOp{
			Item:                lookup,
			ConditionExpression: "attribute_not_exists(PK) AND attribute_not_exists(SK)",
		}},
	})
	if err == nil {
		return nil
	}
	if errors.Is(err, kv.ErrConditionFailed) {
		return ErrEmailTaken
	}
	return err
}

// GetByEmail resolves email→profile via the lookup row.
func (r *DDBUsers) GetByEmail(ctx context.Context, email string) (*user.User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, errors.New("user: email required")
	}
	var lookup emailLookupRow
	if err := r.KV.Get(ctx, kv.Key{PK: EmailLookupPK(email), SK: EmailLookupSK}, &lookup); err != nil {
		return nil, err
	}
	return r.GetByID(ctx, lookup.UserID)
}

// GetByID fetches a profile by user id.
func (r *DDBUsers) GetByID(ctx context.Context, userID string) (*user.User, error) {
	if userID == "" {
		return nil, errors.New("user: id required")
	}
	var row userRow
	if err := r.KV.Get(ctx, kv.Key{PK: UserPK(userID), SK: UserProfileSK}, &row); err != nil {
		return nil, err
	}
	roles := make([]user.Role, len(row.Roles))
	for i, r := range row.Roles {
		roles[i] = user.Role(r)
	}
	return &user.User{
		ID:           row.UserID,
		TenantID:     row.TenantID,
		Email:        row.Email,
		PasswordHash: row.PasswordHash,
		Roles:        roles,
		CreatedAt:    row.CreatedAt,
	}, nil
}
