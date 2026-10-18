import { readFileSync } from "node:fs";
import { expect, it } from "vitest";

// Vitest blanks CSS imports, so the stylesheet is read as text (cwd is dashboard/).
const css = readFileSync("src/styles/index.css", "utf8");

it("declares the UA colour scheme for both themes", () => {
  expect(css).toMatch(/:root\s*{[^}]*color-scheme:\s*light/);
  expect(css).toMatch(/\.dark\s*{[^}]*color-scheme:\s*dark/);
});
