//go:build integration

package admin_test

import (
	"context"
	"testing"
	"time"

	"github.com/dtszwai/media-processing-service/backend/internal/app/admin"
	sqsdrv "github.com/dtszwai/media-processing-service/backend/internal/infra/eventbus/sqs"
	"github.com/dtszwai/media-processing-service/backend/internal/testkit"
)

func TestDLQAdmin_PeekAndReplay(t *testing.T) {
	testkit.SkipIfIntegrationDisabled(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := testkit.SQSClient(t)
	transport := sqsdrv.New(client, "")

	stamp := time.Now().UTC().Format("20060102150405")
	srcURL, err := sqsdrv.EnsureQueue(ctx, client, "admin-src-"+stamp)
	if err != nil {
		t.Fatalf("ensure src: %v", err)
	}
	dlqURL, err := sqsdrv.EnsureQueue(ctx, client, "admin-dlq-"+stamp)
	if err != nil {
		t.Fatalf("ensure dlq: %v", err)
	}
	if _, err := transport.Send(ctx, dlqURL, []byte(`{"poisoned":"payload"}`), nil); err != nil {
		t.Fatalf("seed dlq: %v", err)
	}

	a := admin.NewDLQAdmin(transport, nil, nil)
	a.SetTopology([]byte("test-secret"), map[string]admin.DLQInfo{
		"admin-dlq": {Name: "admin-dlq", URL: dlqURL, SourceURL: srcURL},
	})

	msgs, err := a.Peek(ctx, "admin-dlq", 5)
	if err != nil {
		t.Fatalf("Peek: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("expected 1 dlq message, got %d", len(msgs))
	}
	if msgs[0].Body != `{"poisoned":"payload"}` || msgs[0].BodySignature == "" {
		t.Fatalf("peek body or signature missing: %+v", msgs[0])
	}

	newID, err := a.Replay(ctx, "admin-dlq", admin.DLQMessageInput{
		ID:                msgs[0].ID,
		ReceiptHandle:     msgs[0].ReceiptHandle,
		Body:              msgs[0].Body,
		MessageAttributes: msgs[0].MessageAttributes,
		BodySignature:     msgs[0].BodySignature,
	})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if newID == "" {
		t.Fatalf("missing new message id")
	}

	srcMsgs, err := sqsdrv.New(client, srcURL).Receive(ctx, 1, 2*time.Second)
	if err != nil {
		t.Fatalf("recv src: %v", err)
	}
	if len(srcMsgs) != 1 || string(srcMsgs[0].Body) != `{"poisoned":"payload"}` {
		t.Fatalf("replay did not land in source: %+v", srcMsgs)
	}
}

func TestDLQAdmin_RejectsTamperedSignature(t *testing.T) {
	testkit.SkipIfIntegrationDisabled(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client := testkit.SQSClient(t)
	transport := sqsdrv.New(client, "")

	stamp := time.Now().UTC().Format("20060102150405")
	srcURL, _ := sqsdrv.EnsureQueue(ctx, client, "admin-src2-"+stamp)
	dlqURL, _ := sqsdrv.EnsureQueue(ctx, client, "admin-dlq2-"+stamp)
	_, _ = transport.Send(ctx, dlqURL, []byte("orig"), nil)

	a := admin.NewDLQAdmin(transport, nil, nil)
	a.SetTopology([]byte("test-secret"), map[string]admin.DLQInfo{
		"dlq": {Name: "dlq", URL: dlqURL, SourceURL: srcURL},
	})
	msgs, _ := a.Peek(ctx, "dlq", 1)
	if _, err := a.Replay(ctx, "dlq", admin.DLQMessageInput{
		ID: msgs[0].ID, ReceiptHandle: msgs[0].ReceiptHandle,
		Body:          "tampered",
		BodySignature: msgs[0].BodySignature,
	}); err == nil {
		t.Fatal("expected INVALID_SIGNATURE on tampered body")
	}
}
