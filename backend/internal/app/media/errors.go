package media

import "errors"

// ErrInvalidInput marks a request-validation failure: missing required
// fields, duplicate or unknown enum values, malformed payloads. Maps to
// connect.CodeInvalidArgument at the transport boundary.
var ErrInvalidInput = errors.New("media: invalid input")

// ErrPreconditionFailed marks a state-based rejection: a parent is
// soft-deleted, an asset is not yet ready, a derive operation is invalid for
// the current media type. Maps to
// connect.CodeFailedPrecondition at the transport boundary.
var ErrPreconditionFailed = errors.New("media: precondition failed")
