package com.mediaservice.providers.generation.prompt;

import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.security.SecureRandom;
import java.util.Base64;
import javax.crypto.Cipher;
import javax.crypto.spec.GCMParameterSpec;
import javax.crypto.spec.SecretKeySpec;

/**
 * AES-256-GCM envelope encryption for prompt fields. Wire format {@code enc:v1:<base64(iv||ct||tag)>};
 * the marker lets pre-encryption plaintext rows round-trip. Passthrough when
 * {@code GENERATION_PROMPT_ENCRYPTION_KEY} is unset.
 */
public final class PromptCipher {
  private static final String MARKER = "enc:v1:";
  private static final int IV_BYTES = 12;
  private static final int TAG_BITS = 128;

  private static volatile PromptCipher instance;

  private final SecretKeySpec key;
  private final SecureRandom rng = new SecureRandom();

  private PromptCipher(SecretKeySpec key) {
    this.key = key;
  }

  public static PromptCipher get() {
    PromptCipher local = instance;
    if (local == null) {
      synchronized (PromptCipher.class) {
        if (instance == null) {
          instance = fromEnvironment(System.getenv());
        }
        local = instance;
      }
    }
    return local;
  }

  /** Visible for tests — reset the singleton so a fresh env can be re-evaluated. */
  static void resetForTests() {
    instance = null;
  }

  public static PromptCipher fromEnvironment(java.util.Map<String, String> env) {
    String b64 = env.get("GENERATION_PROMPT_ENCRYPTION_KEY");
    if (b64 == null || b64.isBlank()) {
      return new PromptCipher(null);
    }
    byte[] raw;
    try {
      raw = Base64.getDecoder().decode(b64.trim());
    } catch (IllegalArgumentException e) {
      throw new IllegalStateException("GENERATION_PROMPT_ENCRYPTION_KEY is not valid base64", e);
    }
    if (raw.length != 16 && raw.length != 24 && raw.length != 32) {
      throw new IllegalStateException(
          "GENERATION_PROMPT_ENCRYPTION_KEY must decode to 16, 24, or 32 bytes; got " + raw.length);
    }
    return new PromptCipher(new SecretKeySpec(raw, "AES"));
  }

  public boolean enabled() {
    return key != null;
  }

  public String encrypt(String plaintext) {
    if (plaintext == null || !enabled()) {
      return plaintext;
    }
    if (plaintext.startsWith(MARKER)) {
      return plaintext;
    }
    try {
      byte[] iv = new byte[IV_BYTES];
      rng.nextBytes(iv);
      Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
      cipher.init(Cipher.ENCRYPT_MODE, key, new GCMParameterSpec(TAG_BITS, iv));
      byte[] cipherText = cipher.doFinal(plaintext.getBytes(StandardCharsets.UTF_8));
      ByteBuffer buf = ByteBuffer.allocate(IV_BYTES + cipherText.length);
      buf.put(iv);
      buf.put(cipherText);
      return MARKER + Base64.getEncoder().encodeToString(buf.array());
    } catch (Exception e) {
      throw new IllegalStateException("Failed to encrypt prompt", e);
    }
  }

  public String decrypt(String stored) {
    if (stored == null || !stored.startsWith(MARKER)) {
      return stored;
    }
    if (!enabled()) {
      throw new IllegalStateException(
          "Encountered encrypted prompt but GENERATION_PROMPT_ENCRYPTION_KEY is not configured");
    }
    try {
      byte[] all = Base64.getDecoder().decode(stored.substring(MARKER.length()));
      if (all.length <= IV_BYTES) {
        throw new IllegalStateException("Encrypted prompt payload is shorter than IV");
      }
      byte[] iv = new byte[IV_BYTES];
      System.arraycopy(all, 0, iv, 0, IV_BYTES);
      byte[] cipherText = new byte[all.length - IV_BYTES];
      System.arraycopy(all, IV_BYTES, cipherText, 0, cipherText.length);
      Cipher cipher = Cipher.getInstance("AES/GCM/NoPadding");
      cipher.init(Cipher.DECRYPT_MODE, key, new GCMParameterSpec(TAG_BITS, iv));
      return new String(cipher.doFinal(cipherText), StandardCharsets.UTF_8);
    } catch (Exception e) {
      throw new IllegalStateException("Failed to decrypt prompt", e);
    }
  }
}
