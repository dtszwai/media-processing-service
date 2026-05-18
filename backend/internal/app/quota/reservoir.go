package quota

import (
	"errors"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// ErrQuotaExhausted is the sentinel a reservation returns when the aggregate
// `available` attribute is short of the requested amount. Callers branch on
// errors.Is(err, ErrQuotaExhausted) and the workflow renders the branch as a
// terminal QUOTA_EXHAUSTED job error.
var ErrQuotaExhausted = errors.New("quota: reservoir exhausted")

// Repo is the Reservoir storage repo. The wire format and conditional-update
// expressions are owned here; callers stay in domain types.
type Repo struct {
	KV  kv.KV
	Now func() time.Time
}

// NewRepo binds the repo to a kv driver.
func NewRepo(k kv.KV) *Repo {
	return &Repo{KV: k, Now: func() time.Time { return time.Now().UTC() }}
}

type CapOverride struct {
	PreviousCap      int64
	NewCap           int64
	ReservoirVersion int64
}
