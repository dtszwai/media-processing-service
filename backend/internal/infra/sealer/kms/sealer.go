// Package kms is the AWS KMS driver for the sealer port.
//
// Wire format:
//
//	enc:v1: | len(ciphertextKey):u32 | ciphertextKey | nonce[12] | aesgcm(plaintext)
package kms

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/aws/aws-sdk-go-v2/aws"
	awskms "github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
)

// Sealer is the KMS driver. Each Seal generates a fresh data key bound to
// EncryptionContext={tenant_id, job_id} so ciphertext only decrypts under the
// same tenant+job; rotation happens at the CMK level.
type Sealer struct {
	c     *awskms.Client
	keyID string
}

// New binds the driver to one CMK.
func New(c *awskms.Client, keyID string) *Sealer {
	return &Sealer{c: c, keyID: keyID}
}

const sealMarker = "enc:v1:"

func encCtx(tenantID, jobID string) map[string]string {
	return map[string]string{"tenant_id": tenantID, "job_id": jobID}
}

func (s *Sealer) Seal(ctx context.Context, tenantID, jobID, plaintext string) ([]byte, error) {
	if plaintext == "" {
		return nil, nil
	}
	out, err := s.c.GenerateDataKey(ctx, &awskms.GenerateDataKeyInput{
		KeyId:             aws.String(s.keyID),
		KeySpec:           types.DataKeySpecAes256,
		EncryptionContext: encCtx(tenantID, jobID),
	})
	if err != nil {
		return nil, fmt.Errorf("kms: GenerateDataKey: %w", err)
	}
	defer wipe(out.Plaintext)
	block, err := aes.NewCipher(out.Plaintext)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	ct := gcm.Seal(nil, nonce, []byte(plaintext), nil)

	var buf bytes.Buffer
	buf.WriteString(sealMarker)
	_ = binary.Write(&buf, binary.BigEndian, uint32(len(out.CiphertextBlob)))
	buf.Write(out.CiphertextBlob)
	buf.Write(nonce)
	buf.Write(ct)
	return buf.Bytes(), nil
}

func (s *Sealer) Unseal(ctx context.Context, tenantID, jobID string, blob []byte) (string, error) {
	if len(blob) == 0 {
		return "", nil
	}
	if !bytes.HasPrefix(blob, []byte(sealMarker)) {
		return "", errors.New("kms: unsupported seal marker")
	}
	r := bytes.NewReader(blob[len(sealMarker):])
	var keyLen uint32
	if err := binary.Read(r, binary.BigEndian, &keyLen); err != nil {
		return "", err
	}
	keyBlob := make([]byte, keyLen)
	if _, err := io.ReadFull(r, keyBlob); err != nil {
		return "", err
	}
	nonce := make([]byte, 12)
	if _, err := io.ReadFull(r, nonce); err != nil {
		return "", err
	}
	rest, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	dec, err := s.c.Decrypt(ctx, &awskms.DecryptInput{
		CiphertextBlob:    keyBlob,
		EncryptionContext: encCtx(tenantID, jobID),
	})
	if err != nil {
		return "", fmt.Errorf("kms: Decrypt: %w", err)
	}
	defer wipe(dec.Plaintext)
	block, err := aes.NewCipher(dec.Plaintext)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	pt, err := gcm.Open(nil, nonce, rest, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

func wipe(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
