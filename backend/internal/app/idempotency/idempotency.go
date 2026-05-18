// Package idempotency owns a leased-ownership primitive: stake a claim before
// running an external side-effect, then transition the claim to a terminal
// state (COMPLETED or FAILED) bracketing the work. The contract is:
//
//   - Only one worker can hold an active claim for a given scope. A second
//     attempt while the lease is fresh sees REPLAY_CLAIMED_FRESH; after the
//     lease expires the next attempt may reclaim with REPLAY_CLAIMED_STALE.
//   - Terminal states are sticky. A COMPLETED claim replays the cached
//     result; a FAILED claim is reported as permanently failed — there is no
//     reset operation, so re-stakes against the same scope+hash loop.
//   - Same scope + different input hash surfaces as a usage error
//     (OutcomeConflict / AcquireInputConflict): the caller mixed two
//     different requests into one idempotency key.
//
// The package depends on nothing in app/* or infra/*; concrete Store
// implementations are owned elsewhere.
package idempotency

import (
	"context"
	"fmt"
	"time"
)

// Outcome reports the result of a Claim attempt. The set is intentionally
// larger than NEW/REPLAY/CONFLICT so callers can distinguish in-flight
// concurrent attempts (transient retry) from stale leases (reclaim) from
// completed results (cache hit) without an extra read.
type Outcome string

const (
	OutcomeNew                Outcome = "NEW"
	OutcomeReplayCompleted    Outcome = "REPLAY_COMPLETED"
	OutcomeReplayClaimedFresh Outcome = "REPLAY_CLAIMED_FRESH"
	OutcomeReplayClaimedStale Outcome = "REPLAY_CLAIMED_STALE"
	OutcomeReplayFailed       Outcome = "REPLAY_FAILED"
	OutcomeConflict           Outcome = "CONFLICT"
)

// Status is the persisted row state.
type Status string

const (
	StatusClaimed   Status = "CLAIMED"
	StatusCompleted Status = "COMPLETED"
	StatusFailed    Status = "FAILED"
)

// Store brackets every external side-effect with a leased ownership claim.
// Complete/Fail are conditional on the claim token so a crashed worker's
// stale lease cannot terminally write after another worker reclaimed it.
type Store interface {
	// Claim attempts to stake or replay a claim. On OutcomeNew the returned
	// token is the caller's ownership proof for Complete/Fail.
	Claim(ctx context.Context, scope, inputHash string, lease time.Duration) (Outcome, string, error)

	// Complete writes COMPLETED + result. Conditional on the claim token.
	Complete(ctx context.Context, scope, token, ref string) error

	// Fail writes FAILED + error code. Conditional on the claim token.
	Fail(ctx context.Context, scope, token, errorCode string) error

	// GetResult fetches the persisted ref + status without a claim attempt.
	// Used on REPLAY_COMPLETED paths to retrieve the cached result.
	GetResult(ctx context.Context, scope string) (ref string, status Status, err error)

	// Reclaim swaps the claim token to a fresh one with an extended lease,
	// conditional on lease_until <= now. Used after OutcomeReplayClaimedStale
	// when the original worker has gone away.
	Reclaim(ctx context.Context, scope string, lease time.Duration) (newToken string, err error)

	// Abandon drops the active claim so the next Claim sees the row as if it
	// never existed. Used by the workflow on transient errors within the same
	// invocation so retries don't have to wait for the lease to expire.
	Abandon(ctx context.Context, scope, token string) error
}

// AcquireKind is the classified result of Acquire. Callers translate it into
// their own error semantics.
type AcquireKind int

const (
	// AcquireOwned means the caller holds Token and must run the side effect.
	AcquireOwned AcquireKind = iota
	// AcquireReclaimed means a stale lease was reclaimed; caller holds Token.
	AcquireReclaimed
	// AcquireCompleted means a previous attempt already completed; CachedRef
	// is set to the persisted result.
	AcquireCompleted
	// AcquireInFlight means another worker holds a fresh, unexpired claim.
	AcquireInFlight
	// AcquirePermanentlyFailed means a previous attempt persisted a terminal
	// failure on the same scope+hash. There is no automatic recovery — Store
	// has no "reset" operation, so re-Claiming returns the same outcome.
	AcquirePermanentlyFailed
	// AcquireInputConflict means the scope is in use with a different input
	// hash. Caller-supplied input does not match the cached input.
	AcquireInputConflict
)

// Acquired bundles the classified Claim result.
type Acquired struct {
	Kind      AcquireKind
	Token     string
	CachedRef string
}

// Acquire stakes (or reclaims, or replays) a claim and classifies the
// outcome. Acquire DOES NOT recover from AcquirePermanentlyFailed — the
// persisted row is terminal; callers should surface this to the operator and
// add an explicit reset operation if recovery is desired. Re-Claiming a
// permanently-failed row would loop on the same outcome and is intentionally
// not done here.
func Acquire(ctx context.Context, store Store, scope, inputHash string, lease time.Duration) (Acquired, error) {
	outcome, token, err := store.Claim(ctx, scope, inputHash, lease)
	if err != nil {
		return Acquired{}, fmt.Errorf("idempotency: claim %s: %w", scope, err)
	}
	switch outcome {
	case OutcomeNew:
		return Acquired{Kind: AcquireOwned, Token: token}, nil
	case OutcomeReplayCompleted:
		ref, _, gerr := store.GetResult(ctx, scope)
		if gerr != nil {
			return Acquired{}, fmt.Errorf("idempotency: get cached result %s: %w", scope, gerr)
		}
		return Acquired{Kind: AcquireCompleted, CachedRef: ref}, nil
	case OutcomeReplayClaimedFresh:
		return Acquired{Kind: AcquireInFlight}, nil
	case OutcomeReplayClaimedStale:
		newTok, rerr := store.Reclaim(ctx, scope, lease)
		if rerr != nil {
			return Acquired{}, fmt.Errorf("idempotency: reclaim %s: %w", scope, rerr)
		}
		return Acquired{Kind: AcquireReclaimed, Token: newTok}, nil
	case OutcomeReplayFailed:
		return Acquired{Kind: AcquirePermanentlyFailed}, nil
	case OutcomeConflict:
		return Acquired{Kind: AcquireInputConflict}, nil
	}
	return Acquired{}, fmt.Errorf("idempotency: unknown outcome %q for %s", outcome, scope)
}
