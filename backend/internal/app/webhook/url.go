package webhook

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// NormalizeURL validates a customer webhook endpoint and returns the trimmed
// URL that should be persisted and later dispatched.
func NormalizeURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("webhook: invalid url: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "https") {
		return "", errors.New("webhook: invalid url: scheme must be https")
	}
	if u.Host == "" {
		return "", errors.New("webhook: invalid url: host required")
	}
	return trimmed, nil
}
