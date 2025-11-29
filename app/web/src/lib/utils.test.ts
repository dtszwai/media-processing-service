import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { formatFileSize, formatRelativeTime, formatDateTime } from './utils';

describe('formatFileSize', () => {
  it('formats bytes', () => {
    expect(formatFileSize(500)).toBe('500 B');
    expect(formatFileSize(0)).toBe('0 B');
  });

  it('formats kilobytes', () => {
    expect(formatFileSize(1024)).toBe('1.0 KB');
    expect(formatFileSize(1536)).toBe('1.5 KB');
  });

  it('formats megabytes', () => {
    expect(formatFileSize(1024 * 1024)).toBe('1.0 MB');
    expect(formatFileSize(5.5 * 1024 * 1024)).toBe('5.5 MB');
  });

  it('formats gigabytes', () => {
    expect(formatFileSize(1024 * 1024 * 1024)).toBe('1.00 GB');
    expect(formatFileSize(2.5 * 1024 * 1024 * 1024)).toBe('2.50 GB');
  });
});

describe('formatRelativeTime', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2024-01-15T12:00:00Z'));
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it('returns "Just now" for recent times', () => {
    expect(formatRelativeTime('2024-01-15T11:59:30Z')).toBe('Just now');
  });

  it('returns minutes ago', () => {
    expect(formatRelativeTime('2024-01-15T11:55:00Z')).toBe('5m ago');
    expect(formatRelativeTime('2024-01-15T11:30:00Z')).toBe('30m ago');
  });

  it('returns hours ago', () => {
    expect(formatRelativeTime('2024-01-15T10:00:00Z')).toBe('2h ago');
    expect(formatRelativeTime('2024-01-15T00:00:00Z')).toBe('12h ago');
  });

  it('returns days ago', () => {
    expect(formatRelativeTime('2024-01-14T12:00:00Z')).toBe('1d ago');
    expect(formatRelativeTime('2024-01-12T12:00:00Z')).toBe('3d ago');
  });

  it('returns formatted date for older times', () => {
    const result = formatRelativeTime('2024-01-01T12:00:00Z');
    // Format varies by locale (e.g., "1/1/2024" or "2024-01-01")
    expect(result).toMatch(/\d{4}|\d{1,2}/);
  });
});

describe('formatDateTime', () => {
  it('returns "N/A" for undefined', () => {
    expect(formatDateTime(undefined)).toBe('N/A');
  });

  it('formats valid date strings', () => {
    const result = formatDateTime('2024-01-15T12:30:00Z');
    expect(result).toBeTruthy();
    expect(result).not.toBe('N/A');
  });
});
