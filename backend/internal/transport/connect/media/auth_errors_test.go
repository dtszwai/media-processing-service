package media

import (
	"errors"
	"fmt"
	"testing"

	connect "connectrpc.com/connect"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
)

func TestAppError_NilReturnsNil(t *testing.T) {
	if got := appError(nil); got != nil {
		t.Fatalf("appError(nil) = %v, want nil", got)
	}
}

// TestAppError_SentinelMapping verifies every typed sentinel maps to its
// expected Connect code both bare and wrapped via %w. The trailing cases
// assert that the substring fallback is gone: plain errors carrying the
// old "required" / "not found" / "exceeds" / "not supported" tokens now
// fall through to CodeInternal instead of being silently re-classified.
func TestAppError_SentinelMapping(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want connect.Code
	}{
		{"forbidden_bare", mediaapp.ErrForbidden, connect.CodePermissionDenied},
		{"forbidden_wrapped", fmt.Errorf("%w: user=%s", mediaapp.ErrForbidden, "u1"), connect.CodePermissionDenied},
		{"no_asset_for_role", mediaapp.ErrNoAssetForRole, connect.CodeNotFound},
		{"idempotency_key_reused", mediaapp.ErrIdempotencyKeyReused, connect.CodeAlreadyExists},
		{"invalid_operation_bare", mediaapp.ErrInvalidOperation, connect.CodeInvalidArgument},
		{"invalid_operation_wrapped", fmt.Errorf("%w: %q", mediaapp.ErrInvalidOperation, "foo"), connect.CodeInvalidArgument},
		{"invalid_input_bare", mediaapp.ErrInvalidInput, connect.CodeInvalidArgument},
		{"invalid_input_wrapped", fmt.Errorf("%w: tenant_id required", mediaapp.ErrInvalidInput), connect.CodeInvalidArgument},
		{"retry_exhausted", mediaapp.ErrRetryExhausted, connect.CodeFailedPrecondition},
		{"precondition_failed_bare", mediaapp.ErrPreconditionFailed, connect.CodeFailedPrecondition},
		{"precondition_failed_wrapped", fmt.Errorf("%w: media is soft-deleted", mediaapp.ErrPreconditionFailed), connect.CodeFailedPrecondition},
		{"unknown_internal", errors.New("backend explosion"), connect.CodeInternal},
		{"unknown_required_substring", errors.New("foo bar required"), connect.CodeInternal},
		{"unknown_not_found_substring", errors.New("widget not found"), connect.CodeInternal},
		{"unknown_exceeds_substring", errors.New("limit exceeds 100"), connect.CodeInternal},
		{"unknown_not_supported_substring", errors.New("op not supported"), connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := appError(tc.err)
			var connectErr *connect.Error
			if !errors.As(got, &connectErr) {
				t.Fatalf("appError(%v) returned non-connect error: %v", tc.err, got)
			}
			if connectErr.Code() != tc.want {
				t.Fatalf("appError(%v) code = %v, want %v", tc.err, connectErr.Code(), tc.want)
			}
		})
	}
}
