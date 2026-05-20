import { createCipheriv } from "node:crypto";
import { describe, expect, it } from "vitest";
import { decryptSealedPrompt } from "./prompt-crypto";

function sealFixture(plaintext: string, keyBlob: Buffer, dataKey: Buffer): Buffer {
  const nonce = Buffer.alloc(12, 0x07);
  const cipher = createCipheriv("aes-256-gcm", dataKey, nonce);
  const ciphertext = Buffer.concat([cipher.update(plaintext, "utf8"), cipher.final()]);
  const tag = cipher.getAuthTag();
  const header = Buffer.alloc(4);
  header.writeUInt32BE(keyBlob.length, 0);
  return Buffer.concat([
    Buffer.from("enc:v1:", "utf8"),
    header,
    keyBlob,
    nonce,
    ciphertext,
    tag,
  ]);
}

describe("decryptSealedPrompt", () => {
  it("parses the Go KMS envelope and decrypts AES-GCM payloads", async () => {
    const dataKey = Buffer.alloc(32, 0x42);
    const keyBlob = Buffer.from("encrypted-data-key");
    const sealed = sealFixture("prompt text", keyBlob, dataKey);
    const calls: unknown[] = [];

    const got = await decryptSealedPrompt(
      sealed,
      { tenantId: "tenant-a", jobId: "job-a" },
      async (encryptedDataKey, context) => {
        calls.push({ encryptedDataKey: Buffer.from(encryptedDataKey).toString("utf8"), context });
        return dataKey;
      },
    );

    expect(got).toBe("prompt text");
    expect(calls).toEqual([
      {
        encryptedDataKey: "encrypted-data-key",
        context: { tenantId: "tenant-a", jobId: "job-a" },
      },
    ]);
  });

  it("rejects unsupported envelopes before calling the data-key decryptor", async () => {
    let called = false;
    await expect(
      decryptSealedPrompt(
        Buffer.from("plain"),
        { tenantId: "tenant-a", jobId: "job-a" },
        async () => {
          called = true;
          return Buffer.alloc(32);
        },
      ),
    ).rejects.toThrow("unsupported prompt seal marker");
    expect(called).toBe(false);
  });
});
