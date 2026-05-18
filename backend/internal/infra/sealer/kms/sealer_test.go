package kms

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
)

func TestSealPrefixesCiphertextWithMarkerAndUnseals(t *testing.T) {
	plainKey := bytes.Repeat([]byte{0x42}, 32)
	keyBlob := []byte("encrypted-data-key")
	var targets []string

	client := awskms.NewFromConfig(aws.Config{
		Region:       "us-east-1",
		BaseEndpoint: aws.String("https://kms.test"),
		Credentials:  credentials.NewStaticCredentialsProvider("access", "secret", ""),
		HTTPClient: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			target := req.Header.Get("X-Amz-Target")
			targets = append(targets, target)
			var body map[string]any
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				t.Fatalf("decode request body: %v", err)
			}
			assertEncryptionContext(t, body)

			switch target {
			case "TrentService.GenerateDataKey":
				if got := body["KeySpec"]; got != "AES_256" {
					t.Fatalf("GenerateDataKey KeySpec = %v, want AES_256", got)
				}
				return jsonResponse(map[string]string{
					"CiphertextBlob": base64.StdEncoding.EncodeToString(keyBlob),
					"Plaintext":      base64.StdEncoding.EncodeToString(plainKey),
				}), nil
			case "TrentService.Decrypt":
				if got := body["CiphertextBlob"]; got != base64.StdEncoding.EncodeToString(keyBlob) {
					t.Fatalf("Decrypt CiphertextBlob = %v, want encoded key blob", got)
				}
				return jsonResponse(map[string]string{
					"Plaintext": base64.StdEncoding.EncodeToString(plainKey),
				}), nil
			default:
				t.Fatalf("unexpected KMS target %q", target)
				return nil, nil
			}
		}),
	})

	sealer := New(client, "test-key")
	blob, err := sealer.Seal(context.Background(), "tenant-a", "job-a", "prompt text")
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if !bytes.HasPrefix(blob, []byte(sealMarker)) {
		t.Fatalf("sealed blob prefix = %q, want %q", blob[:len(sealMarker)], sealMarker)
	}
	got, err := sealer.Unseal(context.Background(), "tenant-a", "job-a", blob)
	if err != nil {
		t.Fatalf("Unseal: %v", err)
	}
	if got != "prompt text" {
		t.Fatalf("Unseal = %q, want prompt text", got)
	}
	if len(targets) != 2 {
		t.Fatalf("KMS calls = %d, want 2", len(targets))
	}
}

func TestUnsealRejectsMissingMarker(t *testing.T) {
	client := awskms.NewFromConfig(aws.Config{
		Region:       "us-east-1",
		BaseEndpoint: aws.String("https://kms.test"),
		Credentials:  credentials.NewStaticCredentialsProvider("access", "secret", ""),
		HTTPClient: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			t.Fatalf("KMS should not be called for an unsupported seal marker")
			return nil, nil
		}),
	})

	_, err := New(client, "test-key").Unseal(context.Background(), "tenant-a", "job-a", []byte{0x01, 0x00, 0x00, 0x00, 0x00})
	if err == nil {
		t.Fatal("Unseal succeeded for missing marker")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) Do(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(v any) *http.Response {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(v); err != nil {
		panic(err)
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header: http.Header{
			"Content-Type":     []string{"application/x-amz-json-1.1"},
			"X-Amzn-Requestid": []string{"test-request"},
		},
		Body: io.NopCloser(&buf),
	}
}

func assertEncryptionContext(t *testing.T, body map[string]any) {
	t.Helper()
	ctx, ok := body["EncryptionContext"].(map[string]any)
	if !ok {
		t.Fatalf("EncryptionContext = %#v", body["EncryptionContext"])
	}
	if got := ctx["tenant_id"]; got != "tenant-a" {
		t.Fatalf("tenant_id context = %v, want tenant-a", got)
	}
	if got := ctx["job_id"]; got != "job-a" {
		t.Fatalf("job_id context = %v, want job-a", got)
	}
}
