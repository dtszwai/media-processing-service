package main

import (
	"context"
	"time"

	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
)

// resultPresigner reuses the media service's PresignReady gate for the
// generation result-asset presigner contract. Wires mediaapp into the
// generation Connect server's ResultPresigner port without leaking media types
// into the transport package.
type resultPresigner struct {
	svc *mediaapp.Service
}

func (p *resultPresigner) PresignResult(ctx context.Context, tenantID, mediaID, assetID string) (string, time.Time, error) {
	const ttl = 15 * time.Minute
	a, err := p.svc.PresignReady(ctx, tenantID, mediaID, assetID)
	if err != nil {
		return "", time.Time{}, err
	}
	url, err := p.svc.Storage.PresignGet(ctx, a.StorageKey, ttl)
	if err != nil {
		return "", time.Time{}, err
	}
	return url, time.Now().UTC().Add(ttl), nil
}
