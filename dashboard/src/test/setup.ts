import "@testing-library/jest-dom/vitest";
import { cleanup, configure } from "@testing-library/react";
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

// Router scroll restoration calls scrollTo, which jsdom only logs as not implemented.
window.scrollTo = vi.fn();

// react-aria inspects CSS transitions on shared-element updates; jsdom has no
// animations, so an empty list is the honest answer.
HTMLElement.prototype.getAnimations = () => [];

// input-otp probes password-manager overlays with a hit test jsdom does not implement.
document.elementFromPoint = () => null;

// Browsers define focus() on the Document; jsdom does not. react-aria's toast
// region restores focus to whatever a focusout carried as relatedTarget, and
// a dialog closing over a paused toast can make that the document itself.
const documentFocus = (document as Document & { focus?: () => void }).focus;
if (typeof documentFocus !== "function") {
  Object.defineProperty(document, "focus", {
    configurable: true,
    writable: true,
    value: () => {},
  });
}

// Find queries (findBy*) wait out the suite's own load tolerance: the config
// already raises testTimeout for runs on a loaded machine, and a find whose
// default is one second fails inside a test the suite still considers
// healthy — the route under it had simply not painted yet.
configure({ asyncUtilTimeout: 5000 });

afterEach(cleanup);
