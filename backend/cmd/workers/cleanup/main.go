// Package main is the cleanup-worker SQS consumer. Drains MEDIA_CLEANUP
// outbox messages, deletes the S3 object, and marks the asset row DELETED.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dtszwai/media-processing-service/backend/cmd/internal/runtime"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	sqsdrv "github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus/sqs"
	"github.com/dtszwai/media-processing-service/backend/internal/worker"
)

const serviceName = "cleanup-worker"

type cleanupMsg struct {
	EventType  string `json:"event_type"`
	TenantID   string `json:"tenant_id"`
	MediaID    string `json:"media_id"`
	AssetID    string `json:"asset_id"`
	StorageKey string `json:"storage_key"`
}

func main() {
	runtime.RunWorker(serviceName, func(_ context.Context, _ runtime.Bootstrap, res *bootstrap.AWS) worker.Config {
		return worker.Config{
			Service:   serviceName,
			QueueURL:  res.MediaCleanupQueueURL,
			Consumer:  sqsdrv.New(res.SQS, res.MediaCleanupQueueURL),
			PerMsgTTL: 60 * time.Second,
			Handler: func(ctx context.Context, body []byte) error {
				var msg cleanupMsg
				if err := json.Unmarshal(body, &msg); err != nil {
					return fmt.Errorf("decode cleanup message: %w", err)
				}
				if msg.TenantID == "" || msg.MediaID == "" || msg.AssetID == "" || msg.StorageKey == "" {
					return errors.New("cleanup message missing required fields")
				}
				if err := res.Blob.Delete(ctx, msg.StorageKey); err != nil {
					return fmt.Errorf("s3 delete %q: %w", msg.StorageKey, err)
				}
				if err := res.MediaRepo.MarkAssetDeleted(ctx, msg.TenantID, msg.MediaID, msg.AssetID, time.Now().UTC()); err != nil {
					return fmt.Errorf("mark asset deleted (%s/%s/%s): %w", msg.TenantID, msg.MediaID, msg.AssetID, err)
				}
				return nil
			},
		}
	})
}
