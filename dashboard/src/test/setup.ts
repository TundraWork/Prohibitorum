import "@testing-library/jest-dom/vitest";
import { cleanup } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

beforeEach(() => {
  vi.stubGlobal(
    "ResizeObserver",
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  );
  // A destroyed jsdom history keeps rejecting writes it has already queued, so
  // window.history must be replaced before each test rather than reused.
  window.history.replaceState(null, "", "/");
});

Object.defineProperty(window, "matchMedia", {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: vi.fn(),
    removeListener: vi.fn(),
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
    dispatchEvent: vi.fn(),
  }),
});

HTMLElement.prototype.scrollIntoView = vi.fn();

// react-aria inspects CSS transitions on shared-element updates; jsdom has no
// animations, so an empty list is the honest answer.
HTMLElement.prototype.getAnimations = () => [];

// input-otp probes password-manager overlays with a hit test jsdom does not implement.
document.elementFromPoint = () => null;

afterEach(cleanup);
