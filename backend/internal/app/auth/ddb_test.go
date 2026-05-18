package auth

import (
	"context"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/domain/user"
	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

type captureKV struct {
	ops        []kv.WriteOp
	queryPages []kv.QueryResult
	queryReqs  []kv.QueryRequest
}

func (c *captureKV) Put(context.Context, kv.Item, kv.PutOptions) error { return nil }
func (c *captureKV) Get(context.Context, kv.Key, any) error            { return kv.ErrNotFound }
func (c *captureKV) Query(_ context.Context, req kv.QueryRequest) (kv.QueryResult, error) {
	c.queryReqs = append(c.queryReqs, req)
	if len(c.queryPages) == 0 {
		return kv.QueryResult{}, nil
	}
	page := c.queryPages[0]
	c.queryPages = c.queryPages[1:]
	return page, nil
}
func (c *captureKV) Update(context.Context, kv.UpdateOp) error { return nil }
func (c *captureKV) UpdateReturning(context.Context, kv.UpdateOp) (kv.UpdateOutput, error) {
	return kv.UpdateOutput{}, nil
}
func (c *captureKV) Delete(context.Context, kv.DeleteOp) error { return nil }
func (c *captureKV) TransactWrite(_ context.Context, ops []kv.WriteOp) error {
	c.ops = append([]kv.WriteOp(nil), ops...)
	return nil
}

func TestDDBUsersCreateUsesFullConditionalGuards(t *testing.T) {
	store := &captureKV{}
	repo := NewDDBUsers(store)
	err := repo.Create(context.Background(), user.User{
		ID:           "user-1",
		TenantID:     "tenant-1",
		Email:        "admin@example.com",
		PasswordHash: []byte("hash"),
		Roles:        []user.Role{user.RoleAdmin},
		CreatedAt:    time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	assertFullPutGuards(t, store.ops, 2)
}

func TestDDBAPIKeysCreateUsesFullConditionalGuards(t *testing.T) {
	store := &captureKV{}
	repo := NewDDBAPIKeys(store)
	err := repo.Create(context.Background(), user.APIKey{
		ID:        "key-1",
		TenantID:  "tenant-1",
		UserID:    "user-1",
		Name:      "ci",
		CreatedAt: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
	}, "mps_secret")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	assertFullPutGuards(t, store.ops, 2)
}

func TestDDBAPIKeysListByTenantReadsAllPages(t *testing.T) {
	firstResumeKey := kv.Key{PK: TenantAPIKeysPK("tenant-1"), SK: TenantAPIKeySK("key-1")}
	store := &captureKV{queryPages: []kv.QueryResult{
		{
			Items: []kv.Row{tenantAPIKeyRow{row: tenantRow{
				KeyID: "key-1", TenantID: "tenant-1", UserID: "user-1", Name: "primary",
				CreatedAt: time.Date(2026, 5, 17, 12, 0, 0, 0, time.UTC),
			}}},
			LastEvaluatedKey: &firstResumeKey,
		},
		{
			Items: []kv.Row{tenantAPIKeyRow{row: tenantRow{
				KeyID: "key-2", TenantID: "tenant-1", UserID: "user-2", Name: "ci",
				CreatedAt: time.Date(2026, 5, 17, 12, 1, 0, 0, time.UTC),
			}}},
		},
	}}
	repo := NewDDBAPIKeys(store)

	keys, err := repo.ListByTenant(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}
	if len(keys) != 2 || keys[0].ID != "key-1" || keys[1].ID != "key-2" {
		t.Fatalf("keys = %+v, want both pages in order", keys)
	}
	if len(store.queryReqs) != 2 {
		t.Fatalf("query calls = %d, want 2", len(store.queryReqs))
	}
	if store.queryReqs[0].ExclusiveStartKey != nil {
		t.Fatalf("first query start key = %+v, want nil", store.queryReqs[0].ExclusiveStartKey)
	}
	if store.queryReqs[1].ExclusiveStartKey == nil ||
		store.queryReqs[1].ExclusiveStartKey.PK != firstResumeKey.PK ||
		store.queryReqs[1].ExclusiveStartKey.SK != firstResumeKey.SK {
		t.Fatalf("second query start key = %+v, want %+v", store.queryReqs[1].ExclusiveStartKey, firstResumeKey)
	}
}

func assertFullPutGuards(t *testing.T, ops []kv.WriteOp, want int) {
	t.Helper()
	if len(ops) != want {
		t.Fatalf("ops = %d, want %d", len(ops), want)
	}
	for i, op := range ops {
		if op.Put == nil {
			t.Fatalf("op[%d] is not a Put", i)
		}
		if got, want := op.Put.ConditionExpression, "attribute_not_exists(PK) AND attribute_not_exists(SK)"; got != want {
			t.Fatalf("op[%d] condition = %q, want %q", i, got, want)
		}
	}
}

type tenantAPIKeyRow struct {
	row tenantRow
}

func (r tenantAPIKeyRow) Unmarshal(out any) error {
	dst := out.(*tenantRow)
	*dst = r.row
	return nil
}

func (tenantAPIKeyRow) Get(string) any { return nil }
