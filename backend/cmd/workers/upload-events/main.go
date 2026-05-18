// Package main is the upload-events SQS consumer. It drains the
// media-upload-events queue (subscribed to S3 ObjectCreated notifications on
// the media bucket) and routes each object into the same idempotent
// completion FSM the CompletePresignedUpload Connect endpoint uses.
//
// The S3 event path is the failsafe for the crash-after-PUT gap in the
// client-driven completion flow: if the client never calls CompletePresignedUpload
// the row would otherwise sit in PENDING until the 24-hour reaper sweeps
// it. With both paths writing the same
// UPLOAD_COMPLETE#<tenant>#<asset>#<version-or-etag> claim scope, whichever
// arrives first does the work and the other is a no-op.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	lambdaevents "github.com/aws/aws-lambda-go/events"

	"github.com/dtszwai/media-processing-service/backend/cmd/internal/runtime"
	mediaapp "github.com/dtszwai/media-processing-service/backend/internal/app/media"
	"github.com/dtszwai/media-processing-service/backend/internal/bootstrap"
	"github.com/dtszwai/media-processing-service/backend/internal/domain/media"
	sqsdrv "github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus/sqs"
	"github.com/dtszwai/media-processing-service/backend/internal/worker"
)

const serviceName = "upload-events-worker"

func main() {
	runtime.RunWorker(serviceName, func(_ context.Context, _ runtime.Bootstrap, res *bootstrap.AWS) worker.Config {
		svc := mediaapp.NewService(res.MediaRepo, res.Blob)
		return worker.Config{
			Service:   serviceName,
			QueueURL:  res.MediaUploadEventsQueueURL,
			Consumer:  sqsdrv.New(res.SQS, res.MediaUploadEventsQueueURL),
			PerMsgTTL: 60 * time.Second,
			Handler: func(ctx context.Context, body []byte) error {
				return handle(ctx, svc, body)
			},
		}
	})
}

// handle parses one S3 ObjectCreated SQS message and dispatches each record
// through the shared completion core. Test-bucket notifications and
// non-ObjectCreated events are skipped so the queue never DLQs on routine
// chatter from S3.
func handle(ctx context.Context, svc *mediaapp.Service, body []byte) error {
	// S3 sends a test message ("s3:TestEvent") at notification-config creation
	// time. It is not wrapped in the Records shape; skip it explicitly so the
	// JSON decoder doesn't surface a noisy unmarshal error.
	if isS3TestEvent(body) {
		return nil
	}
	var evt lambdaevents.S3Event
	if err := json.Unmarshal(body, &evt); err != nil {
		return fmt.Errorf("decode s3 event: %w", err)
	}
	if len(evt.Records) == 0 {
		return nil
	}
	var firstErr error
	for _, rec := range evt.Records {
		if !strings.HasPrefix(rec.EventName, "ObjectCreated:") {
			continue
		}
		if err := dispatchRecord(ctx, svc, rec); err != nil {
			// Log every record's error but keep handling — one poisoned
			// record shouldn't block the rest of a batch. Returning the first
			// failure lets the worker retry the whole message; idempotency on
			// the successful records makes that safe.
			slog.ErrorContext(ctx, "upload-events: dispatch record",
				"key", rec.S3.Object.Key, "version_id", rec.S3.Object.VersionID, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return firstErr
}

func dispatchRecord(ctx context.Context, svc *mediaapp.Service, rec lambdaevents.S3EventRecord) error {
	key := rec.S3.Object.URLDecodedKey
	if key == "" {
		key = rec.S3.Object.Key
	}
	tenantID, mediaID, assetID, _, ok := media.ParseStorageKey(key)
	if !ok {
		// The bucket is shared with other prefixes (provider-staging, etc.) —
		// silently skip keys that don't match the assets layout so the queue
		// doesn't DLQ on unrelated writes.
		return nil
	}
	in := mediaapp.S3CompleteInput{
		TenantID:         tenantID,
		MediaID:          mediaID,
		AssetID:          assetID,
		StorageKey:       key,
		StorageVersionID: rec.S3.Object.VersionID,
		SizeBytes:        rec.S3.Object.Size,
		ETag:             strings.Trim(rec.S3.Object.ETag, `"`),
	}
	_, err := svc.CompleteUploadFromS3(ctx, in)
	if err == nil {
		return nil
	}
	// Storage-key mismatch surfaces here if the row was rebuilt under a
	// different asset id since this S3 event was queued; that's a benign
	// reconciliation gap rather than a worker error.
	if errors.Is(err, mediaapp.ErrIdempotencyKeyReused) {
		return nil
	}
	return err
}

// isS3TestEvent detects the s3:TestEvent envelope S3 sends once on
// notification-config creation. The envelope is `{"Service":"Amazon
// S3","Event":"s3:TestEvent",…}` — no Records key.
func isS3TestEvent(body []byte) bool {
	var probe struct {
		Event string `json:"Event"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return false
	}
	return probe.Event == "s3:TestEvent"
}
