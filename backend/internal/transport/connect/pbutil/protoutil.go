// Package protoutil contains small helpers for filling proto optional
// (pointer-to-scalar) fields. The helpers encode presence semantics in their
// names: Ptr* always sets the field present, while Opt* omits empty values.
package protoutil

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"
)

// PtrString returns &s. The proto field is present even when s is empty.
func PtrString(s string) *string { return &s }

// OptString returns &s when s is non-empty, otherwise nil.
func OptString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// PtrBool returns &v.
func PtrBool(v bool) *bool { return &v }

// PtrInt32 returns &v.
func PtrInt32(v int32) *int32 { return &v }

// OptTimestamp returns timestamppb.New(t) when t is non-zero, otherwise nil.
func OptTimestamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

// OptTimestampPtr returns nil when t is nil or t.IsZero(), otherwise
// timestamppb.New(*t). Use for optional persisted timestamps modeled as
// *time.Time on the app/domain side.
func OptTimestampPtr(t *time.Time) *timestamppb.Timestamp {
	if t == nil {
		return nil
	}
	return OptTimestamp(*t)
}

// OptParsedTimestampRFC3339Nano parses s as RFC3339Nano, returning nil when
// the persisted optional timestamp is empty or malformed.
func OptParsedTimestampRFC3339Nano(s string) *timestamppb.Timestamp {
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return nil
	}
	return timestamppb.New(t)
}
