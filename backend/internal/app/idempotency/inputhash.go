package idempotency

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
)

// HashInputs returns the canonical hex SHA-256 input_hash argument to
// Store.Claim. Callers stringify their request-defining fields (tenant,
// prompt, model, normalized op list, etc.) at the call site so the encoding
// stays explicit and reviewable; this helper only owns structural framing and
// digesting.
func HashInputs(parts ...string) string {
	var buf bytes.Buffer
	_ = binary.Write(&buf, binary.BigEndian, uint64(len(parts)))
	for _, part := range parts {
		_ = binary.Write(&buf, binary.BigEndian, uint64(len(part)))
		buf.WriteString(part)
	}
	sum := sha256.Sum256(buf.Bytes())
	return hex.EncodeToString(sum[:])
}
