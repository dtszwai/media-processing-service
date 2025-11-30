import { describe, it, expect, beforeEach } from "vitest";
import { get } from "svelte/store";
import { currentMediaId, isProcessing, currentView } from "./stores";

describe("stores", () => {
  beforeEach(() => {
    currentMediaId.set(null);
    isProcessing.set(false);
    currentView.set("upload");
  });

  describe("currentMediaId", () => {
    it("starts as null", () => {
      expect(get(currentMediaId)).toBeNull();
    });

    it("can be set", () => {
      currentMediaId.set("test-id");
      expect(get(currentMediaId)).toBe("test-id");
    });

    it("can be cleared", () => {
      currentMediaId.set("test-id");
      currentMediaId.set(null);
      expect(get(currentMediaId)).toBeNull();
    });
  });

  describe("isProcessing", () => {
    it("starts as false", () => {
      expect(get(isProcessing)).toBe(false);
    });

    it("can be set to true", () => {
      isProcessing.set(true);
      expect(get(isProcessing)).toBe(true);
    });
  });

  describe("currentView", () => {
    it("starts as upload", () => {
      expect(get(currentView)).toBe("upload");
    });

    it("can be set to analytics", () => {
      currentView.set("analytics");
      expect(get(currentView)).toBe("analytics");
    });

    it("can be set back to upload", () => {
      currentView.set("analytics");
      currentView.set("upload");
      expect(get(currentView)).toBe("upload");
    });
  });
});
