import { createDecipheriv } from "node:crypto";

const MARKER = Buffer.from("enc:v1:", "utf8");
const NONCE_BYTES = 12;

export type PromptDecryptContext = {
  tenantId: string;
  jobId: string;
};

export type DataKeyDecryptor = (
  encryptedDataKey: Uint8Array,
  context: PromptDecryptContext,
) => Promise<Uint8Array>;

export async function decryptSealedPrompt(
  sealed: Uint8Array,
  context: PromptDecryptContext,
  decryptDataKey: DataKeyDecryptor,
): Promise<string> {
  const blob = Buffer.from(sealed);
  if (blob.length === 0) return "";
  if (!blob.subarray(0, MARKER.length).equals(MARKER)) {
    throw new Error("unsupported prompt seal marker");
  }

  let offset = MARKER.length;
  if (blob.length < offset + 4) {
    throw new Error("truncated prompt seal header");
  }
  const keyLength = blob.readUInt32BE(offset);
  offset += 4;
  if (keyLength <= 0 || blob.length < offset + keyLength + NONCE_BYTES) {
    throw new Error("truncated prompt seal body");
  }

  const encryptedDataKey = blob.subarray(offset, offset + keyLength);
  offset += keyLength;
  const nonce = blob.subarray(offset, offset + NONCE_BYTES);
  offset += NONCE_BYTES;
  const encrypted = blob.subarray(offset);
  if (encrypted.length < 16) {
    throw new Error("truncated prompt ciphertext");
  }

  const dataKey = Buffer.from(await decryptDataKey(encryptedDataKey, context));
  try {
    const ciphertext = encrypted.subarray(0, encrypted.length - 16);
    const authTag = encrypted.subarray(encrypted.length - 16);
    const decipher = createDecipheriv("aes-256-gcm", dataKey, nonce);
    decipher.setAuthTag(authTag);
    return Buffer.concat([decipher.update(ciphertext), decipher.final()]).toString("utf8");
  } finally {
    dataKey.fill(0);
  }
}
