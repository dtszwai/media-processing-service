// Package quota defines the reservation-ledger domain. The Reservoir is the
// scoped capacity pool (per tenant, API key, vendor, or service), and a
// Reservation is a single in-flight or settled draw against it. The four-state
// reservation lifecycle (RESERVED -> COMMITTED | RELEASED -> RECONCILED) is
// what lets the budgeting layer reconcile provider receipts against estimates
// without losing atomicity on the hot reservation path.
package quota

import "time"

type Metric string

const (
	CostMicroUSD     Metric = "COST_MICRO_USD"
	Requests         Metric = "REQUESTS"
	GeneratedOutputs Metric = "GENERATED_OUTPUTS"
	StorageBytes     Metric = "STORAGE_BYTES"
	ProviderCalls    Metric = "PROVIDER_CALLS"
)

type ScopeType string

const (
	ScopeTenant  ScopeType = "TENANT"
	ScopeAPIKey  ScopeType = "APIKEY"
	ScopeVendor  ScopeType = "VENDOR"
	ScopeService ScopeType = "SERVICE"
)

type ReservoirState string

const (
	ReservoirOpen      ReservoirState = "OPEN"
	ReservoirExhausted ReservoirState = "EXHAUSTED"
	ReservoirClosed    ReservoirState = "CLOSED"
)

// Reservoir is the precomputed-availability ledger row. `Available` is
// maintained as `Cap - Reserved - Committed + Released` by the writer so the
// hot reservation path is a single conditional update against `Available`;
// DynamoDB conditions cannot do arithmetic, so this denormalization is
// load-bearing for atomicity.
type Reservoir struct {
	ScopeType     ScopeType
	ScopeID       string
	Metric        Metric
	Period        string
	Cap           int64
	Available     int64
	Reserved      int64
	Committed     int64
	Released      int64
	State         ReservoirState
	PolicyID      string
	PolicyVersion int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ReservationState string

const (
	ReservationReserved   ReservationState = "RESERVED"
	ReservationCommitted  ReservationState = "COMMITTED"
	ReservationReleased   ReservationState = "RELEASED"
	ReservationReconciled ReservationState = "RECONCILED"
)

// Reservation is one draw against a Reservoir. Reason carries the call-site
// classification so reconciliation can attribute drift to a specific subsystem
// without scanning surrounding rows.
type Reservation struct {
	ID             string
	JobID          string
	MediaID        string
	APIKeyID       string
	Amount         int64
	State          ReservationState
	Reason         string
	PricingVersion string
	CreatedAt      time.Time
	ReservedAt     time.Time
	CommittedAt    *time.Time
	ReleasedAt     *time.Time
}
