package generation

import (
	"context"
	"testing"

	connect "connectrpc.com/connect"

	"github.com/dtszwai/media-processing-service/backend/internal/app/idempotency"
	domaingen "github.com/dtszwai/media-processing-service/backend/internal/domain/generation"
	"github.com/dtszwai/media-processing-service/backend/internal/util/jwtauth"
)

func TestResolveReadJobIDAcceptsGenerationIDOnly(t *testing.T) {
	got, err := resolveReadJobID("", "gen_read_1")
	if err != nil {
		t.Fatalf("resolveReadJobID: %v", err)
	}
	if got != "gen_read_1" {
		t.Fatalf("jobID = %q, want gen_read_1", got)
	}
}

func TestResolveReadJobIDRejectsMismatchedIdentities(t *testing.T) {
	_, err := resolveReadJobID("gen_read_1", "gen_read_2")
	if err == nil {
		t.Fatal("expected mismatch error, got nil")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("code = %s, want InvalidArgument", connect.CodeOf(err))
	}
}

func TestSubmitInputHashIncludesProvider(t *testing.T) {
	simulated := idempotency.HashInputs("tenant-a", "same prompt", "same-model", "IMAGE", "simulated", "FREE", "1024x1024", "0", "1")
	codex := idempotency.HashInputs("tenant-a", "same prompt", "same-model", "IMAGE", "codex", "FREE", "1024x1024", "0", "1")
	if simulated == codex {
		t.Fatalf("submit hash did not change across providers: %s", simulated)
	}
}

func TestSubmitRejectsEmptyModel(t *testing.T) {
	s := &Server{}
	claims := &jwtauth.Claims{Subject: "user-1", TenantID: "tenant-1"}
	_, err := s.submit(context.Background(), claims, generationSubmitSpec{
		OutputType:     domaingen.OutputImage,
		Prompt:         "anything",
		Tier:           "free",
		Provider:       "codex",
		Model:          "",
		IdempotencyKey: "key-1",
	})
	if err == nil {
		t.Fatal("expected InvalidArgument for empty spec.Model, got nil")
	}
	if connect.CodeOf(err) != connect.CodeInvalidArgument {
		t.Fatalf("expected InvalidArgument, got %s: %v", connect.CodeOf(err), err)
	}
}
