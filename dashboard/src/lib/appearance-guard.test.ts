import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import { afterEach, expect, it, vi } from "vitest";
import { appliedAppearanceKey } from "@/lib/appearance";

/*
 * `/appearance-guard.js` runs before the app, from `<head>`, with whatever the
 * app last stored. It is plain script, so it is run here as the page runs it.
 */
const guard = readFileSync(
  join(
    dirname(fileURLToPath(import.meta.url)),
    "../../public/appearance-guard.js",
  ),
  "utf8",
);

function runGuard({ systemDark = false } = {}) {
  vi.spyOn(window, "matchMedia").mockImplementation(
    (query: string) =>
      ({
        matches: systemDark && query === "(prefers-color-scheme: dark)",
      }) as MediaQueryList,
  );
  new Function(guard)();
}

const root = () => document.documentElement;

afterEach(() => {
  vi.restoreAllMocks();
  localStorage.clear();
  delete root().dataset.scheme;
  delete root().dataset.theme;
  root().classList.remove("dark");
});

it("applies the stored scheme and mode", () => {
  localStorage.setItem(
    appliedAppearanceKey,
    JSON.stringify({ scheme: "nord", mode: "dark" }),
  );
  runGuard();
  expect(root().dataset.scheme).toBe("nord");
  expect(root().dataset.theme).toBe("dark");
  expect(root().classList.contains("dark")).toBe(true);
});

it("resolves the system mode against the device", () => {
  localStorage.setItem(
    appliedAppearanceKey,
    JSON.stringify({ scheme: "sand", mode: "system" }),
  );
  runGuard({ systemDark: true });
  expect(root().dataset.theme).toBe("dark");
});

it("keeps light when light is stored, whatever the device", () => {
  localStorage.setItem(
    appliedAppearanceKey,
    JSON.stringify({ scheme: "sand", mode: "light" }),
  );
  runGuard({ systemDark: true });
  expect(root().dataset.theme).toBe("light");
  expect(root().classList.contains("dark")).toBe(false);
});

it("follows the device and leaves the scheme alone with nothing stored", () => {
  runGuard({ systemDark: true });
  expect(root().dataset.scheme).toBeUndefined();
  expect(root().dataset.theme).toBe("dark");
});

it("ignores what it cannot read", () => {
  localStorage.setItem(appliedAppearanceKey, "{not json");
  runGuard();
  expect(root().dataset.theme).toBe("light");
  localStorage.setItem(
    appliedAppearanceKey,
    JSON.stringify({ scheme: '"><script>', mode: 3 }),
  );
  runGuard();
  expect(root().dataset.scheme).toBeUndefined();
});
