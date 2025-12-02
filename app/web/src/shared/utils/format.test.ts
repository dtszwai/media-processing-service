import { describe, it, expect } from "vitest";
import { formatFileSize, formatRelativeTime, formatDateTime, formatNumber } from "./format";

describe("formatFileSize", () => {
  it("formats bytes", () => {
    expect(formatFileSize(500)).toBe("500 B");
    expect(formatFileSize(0)).toBe("0 B");
  });

  it("formats kilobytes", () => {
    expect(formatFileSize(1024)).toBe("1.0 KB");
    expect(formatFileSize(1536)).toBe("1.5 KB");
  });

  it("formats megabytes", () => {
    expect(formatFileSize(1024 * 1024)).toBe("1.0 MB");
    expect(formatFileSize(5.5 * 1024 * 1024)).toBe("5.5 MB");
  });

  it("formats gigabytes", () => {
    expect(formatFileSize(1024 * 1024 * 1024)).toBe("1.00 GB");
    expect(formatFileSize(2.5 * 1024 * 1024 * 1024)).toBe("2.50 GB");
  });
});

describe("formatRelativeTime", () => {
  it("formats recent times as 'Just now'", () => {
    const now = new Date();
    expect(formatRelativeTime(now.toISOString())).toBe("Just now");
  });

  it("formats minutes ago", () => {
    const fiveMinutesAgo = new Date(Date.now() - 5 * 60 * 1000);
    expect(formatRelativeTime(fiveMinutesAgo.toISOString())).toBe("5m ago");
  });

  it("formats hours ago", () => {
    const threeHoursAgo = new Date(Date.now() - 3 * 60 * 60 * 1000);
    expect(formatRelativeTime(threeHoursAgo.toISOString())).toBe("3h ago");
  });

  it("formats days ago", () => {
    const twoDaysAgo = new Date(Date.now() - 2 * 24 * 60 * 60 * 1000);
    expect(formatRelativeTime(twoDaysAgo.toISOString())).toBe("2d ago");
  });
});

describe("formatDateTime", () => {
  it("returns N/A for undefined", () => {
    expect(formatDateTime(undefined)).toBe("N/A");
  });

  it("formats a valid date string", () => {
    const date = "2024-01-15T10:30:00Z";
    const result = formatDateTime(date);
    expect(result).toBeTruthy();
    expect(result).not.toBe("N/A");
  });
});

describe("formatNumber", () => {
  it("formats numbers with commas", () => {
    expect(formatNumber(1000)).toBe("1,000");
    expect(formatNumber(1000000)).toBe("1,000,000");
  });
});
