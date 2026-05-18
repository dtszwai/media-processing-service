// Package randid issues 32-character hex strings backed by 16 bytes of
// crypto/rand. Used as a single source for media IDs, asset IDs, generation
// job IDs, message IDs, delivery IDs, and inbound request IDs.
package randid

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
)

// New returns 16 random bytes encoded as a 32-character lowercase hex string.
func New() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Errorf("randid: crypto/rand: %w", err))
	}
	return hex.EncodeToString(b[:])
}
