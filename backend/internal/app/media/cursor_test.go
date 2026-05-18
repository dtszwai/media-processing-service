package media

import (
	"testing"

	"github.com/dtszwai/media-processing-service/backend/internal/infra/kv"
)

func TestCursor_RoundTrip(t *testing.T) {
	k := &kv.Key{PK: "TENANT#t1#MEDIA#m1", SK: "MEDIA"}
	encoded := encodeCursor(k)
	if encoded == "" {
		t.Fatal("expected non-empty cursor")
	}
	decoded, err := decodeCursor(encoded)
	if err != nil {
		t.Fatalf("decodeCursor: %v", err)
	}
	if decoded.PK != k.PK || decoded.SK != k.SK {
		t.Fatalf("round-trip mismatch: got %+v, want %+v", decoded, k)
	}
}

func TestCursor_EmptyStringDecodesToNil(t *testing.T) {
	got, err := decodeCursor("")
	if err != nil {
		t.Fatalf("decodeCursor empty: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for empty cursor, got %+v", got)
	}
}

func TestCursor_NilKeyEncodesToEmpty(t *testing.T) {
	if got := encodeCursor(nil); got != "" {
		t.Fatalf("expected empty string for nil key, got %q", got)
	}
}

func TestCursor_MalformedBase64(t *testing.T) {
	_, err := decodeCursor("!!!not-base64!!!")
	if err == nil {
		t.Fatal("expected error for malformed base64")
	}
}

func TestCursor_MalformedJSON(t *testing.T) {
	// "notjson" base64url-encoded
	_, err := decodeCursor("bm90anNvbg")
	if err == nil {
		t.Fatal("expected error for malformed JSON in cursor")
	}
}

func TestCursor_MissingPKOrSK(t *testing.T) {
	// "{}" base64url-encoded — valid JSON but missing PK/SK fields
	_, err := decodeCursor("e30")
	if err == nil {
		t.Fatal("expected error for cursor missing PK/SK")
	}
}
