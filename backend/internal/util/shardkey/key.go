// Package shardkey hashes strings to a shard index. Used by the outbox
// relay, generation submit, media upload FSM, and analytics counters so
// the same input maps to the same shard everywhere.
package shardkey

import (
	"crypto/sha256"
	"encoding/binary"
)

// Of returns sha256(s) mod n. Stable across processes.
func Of(s string, n int) int {
	if n <= 1 {
		return 0
	}
	sum := sha256.Sum256([]byte(s))
	return int(binary.BigEndian.Uint32(sum[:4])) % n
}
