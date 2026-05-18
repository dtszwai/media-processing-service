package media

import "github.com/dtszwai/media-processing-service/backend/internal/infra/kv"

// DDBRepo persists Media + Asset rows on the single table.
type DDBRepo struct {
	KV kv.KV
}

// NewDDBRepo returns the DDB-backed Repository.
func NewDDBRepo(k kv.KV) *DDBRepo { return &DDBRepo{KV: k} }
