import { describe, it, expect, beforeEach } from "vitest";
import { get } from "svelte/store";
import { currentMediaId, isProcessing } from "./index";

describe("stores", () => {
  beforeEach(() => {
    currentMediaId.set(null);
    isProcessing.set(false);
  });

  describe("currentMediaId", () => {
    it("has initial value of null", () => {
      expect(get(currentMediaId)).toBeNull();
    });

    it("can be set to a media ID", () => {
      currentMediaId.set("test-media-123");
      expect(get(currentMediaId)).toBe("test-media-123");
    });

    it("can be reset to null", () => {
      currentMediaId.set("test-media-123");
      currentMediaId.set(null);
      expect(get(currentMediaId)).toBeNull();
    });
  });

  describe("isProcessing", () => {
    it("has initial value of false", () => {
      expect(get(isProcessing)).toBe(false);
    });

    it("can be set to true", () => {
      isProcessing.set(true);
      expect(get(isProcessing)).toBe(true);
    });
  });
});
