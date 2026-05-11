package com.mediaservice.providers.generation.audio;

import java.io.ByteArrayOutputStream;
import java.nio.ByteBuffer;
import java.nio.ByteOrder;
import java.nio.charset.StandardCharsets;

/**
 * WAV byte-stream encoder used by the simulated audio-overview path. Emits a single-tone PCM
 * payload with an ASCII disclosure marker tail so downstream artifact verifiers can sniff the
 * embedded marker.
 */
public final class WavEncoder {
  private static final int BITS_PER_SAMPLE = 16;
  private static final short PCM_FORMAT = 1;
  private static final short CHANNELS = 1;
  private static final int FMT_CHUNK_SIZE = 16;

  private WavEncoder() {}

  /**
   * Encode a single-channel 16-bit PCM tone followed by an ASCII marker tail.
   *
   * @param sampleRate samples per second (e.g. {@code 8000})
   * @param frequencyHz tone frequency in Hz (e.g. {@code 330} or {@code 440})
   * @param amplitude peak short-amplitude (e.g. {@code 9000})
   * @param durationMs total tone duration in milliseconds (rounded down to whole samples)
   * @param marker ASCII/UTF-8 marker bytes appended after the PCM payload
   */
  public static byte[] encode(int sampleRate, int frequencyHz, int amplitude, int durationMs, String marker) {
    byte[] markerBytes = marker.getBytes(StandardCharsets.UTF_8);
    int sampleCount = (int) ((long) sampleRate * durationMs / 1000L);
    byte[] pcm = new byte[sampleCount * 2 + markerBytes.length];
    for (int i = 0; i < sampleCount; i++) {
      double angle = i / ((double) sampleRate / frequencyHz) * 2.0 * Math.PI;
      short value = (short) (Math.sin(angle) * amplitude);
      pcm[i * 2] = (byte) (value & 0xff);
      pcm[i * 2 + 1] = (byte) ((value >> 8) & 0xff);
    }
    System.arraycopy(markerBytes, 0, pcm, sampleCount * 2, markerBytes.length);

    ByteArrayOutputStream out = new ByteArrayOutputStream();
    writeAscii(out, "RIFF");
    writeInt(out, 36 + pcm.length);
    writeAscii(out, "WAVEfmt ");
    writeInt(out, FMT_CHUNK_SIZE);
    writeShort(out, PCM_FORMAT);
    writeShort(out, CHANNELS);
    writeInt(out, sampleRate);
    writeInt(out, sampleRate * 2);
    writeShort(out, (short) 2);
    writeShort(out, (short) BITS_PER_SAMPLE);
    writeAscii(out, "data");
    writeInt(out, pcm.length);
    out.writeBytes(pcm);
    return out.toByteArray();
  }

  private static void writeAscii(ByteArrayOutputStream out, String value) {
    out.writeBytes(value.getBytes(StandardCharsets.US_ASCII));
  }

  private static void writeInt(ByteArrayOutputStream out, int value) {
    out.writeBytes(ByteBuffer.allocate(4).order(ByteOrder.LITTLE_ENDIAN).putInt(value).array());
  }

  private static void writeShort(ByteArrayOutputStream out, short value) {
    out.writeBytes(ByteBuffer.allocate(2).order(ByteOrder.LITTLE_ENDIAN).putShort(value).array());
  }
}
