package media

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

// encodeCursor serialises a DynamoDB LastEvaluatedKey as a URL-safe base64
// JSON string. The caller passes the value directly to the next page request.
// A nil key encodes to "".
func encodeCursor(k *kv.Key) string {
	if k == nil {
		return ""
	}
	b, _ := json.Marshal(k)
	return base64.RawURLEncoding.EncodeToString(b)
}

// decodeCursor reverses encodeCursor. An empty string decodes to nil (= first
// page). Any other value that fails base64 or JSON parsing is treated as a
// caller error.
func decodeCursor(s string) (*kv.Key, error) {
	if s == "" {
		return nil, nil
	}
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed cursor (base64 decode failed)", ErrInvalidInput)
	}
	var k kv.Key
	if err := json.Unmarshal(b, &k); err != nil {
		return nil, fmt.Errorf("%w: malformed cursor (json unmarshal failed)", ErrInvalidInput)
	}
	if k.PK == "" || k.SK == "" {
		return nil, fmt.Errorf("%w: malformed cursor (missing PK or SK)", ErrInvalidInput)
	}
	return &k, nil
}
