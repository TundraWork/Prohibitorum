import { readFileSync } from "node:fs";
import { expect, it } from "vitest";
import {
  capsuleSurfaceStyle,
  cardSurfaceStyle,
  surfaceStyle,
} from "@/components/custom/login-appearance/surface-style";

// Vitest blanks CSS imports, so the stylesheet is read as text (cwd is dashboard/).
const css = readFileSync("src/styles/index.css", "utf8");

function rule(selector: string): string {
  const start = css.indexOf(`${selector} {`);
  expect(start, `no rule for ${selector}`).toBeGreaterThan(-1);
  return css.slice(start, css.indexOf("\n  }", start));
}

it("draws an opaque card with the card colour and nothing else", () => {
  expect(surfaceStyle({ translucent: false, opacity: 40, blur: true })).toEqual(
    { className: "bg-card" },
  );
});

it("fills a translucent card through a rule that exists in the stylesheet", () => {
  const s = surfaceStyle({ translucent: true, opacity: 50, blur: false });
  expect(s.style).toEqual({ "--card-alpha": "50%" });
  expect(s.className.split(" ")).toEqual(["surface-glass"]);
  expect(rule(".surface-glass")).toMatch(
    /background-color:\s*color-mix\([\s\S]*?var\(--card-alpha\)/,
  );
});

it("keeps the fill from reading --background, which the card redefines", () => {
  expect(rule(".surface-glass")).not.toMatch(
    /background-color:[^;]*var\(--background\)/,
  );
  // Taken at the root, before any card redefines it, in every scheme.
  expect(css).toMatch(/:root \{\s*--glass-base: var\(--background\);\s*\}/);
});

it("adds the blur only to a translucent surface with frosted glass on", () => {
  const blurred = surfaceStyle({ translucent: true, opacity: 80, blur: true });
  expect(blurred.className.split(" ")).toEqual(
    expect.arrayContaining(["backdrop-blur-xl", "backdrop-saturate-150"]),
  );
  const sharp = surfaceStyle({ translucent: true, opacity: 80, blur: false });
  expect(sharp.className).not.toContain("backdrop-blur-xl");
  expect(
    surfaceStyle({ translucent: false, opacity: 80, blur: true }).className,
  ).not.toContain("backdrop-blur-xl");
});

it("fills an opaque capsule with the card colour", () => {
  expect(
    capsuleSurfaceStyle({ translucent: false, opacity: 70, blur: true }),
  ).toEqual({ className: "bg-card" });
});

it("gives a translucent capsule the card's fill rule with its own settings", () => {
  const capsule = capsuleSurfaceStyle({
    translucent: true,
    opacity: 30,
    blur: false,
  });
  expect(capsule.style).toEqual({ "--card-alpha": "30%" });
  expect(capsule.className).toContain("surface-glass");
});

it("redefines only the opaque fill tokens inside a translucent card, and an opaque card nothing", () => {
  const glass = cardSurfaceStyle({
    translucent: true,
    opacity: 60,
    blur: false,
  });
  expect(glass.className.split(" ")).toContain("surface-glass-controls");
  expect(glass.style).toEqual({ "--card-alpha": "60%" });
  const controls = rule(".surface-glass-controls");
  expect(controls).toMatch(/--background:\s*color-mix\([\s\S]*?--card-alpha/);
  for (const token of ["border", "input", "muted", "accent"]) {
    expect(controls).not.toContain(`--${token}:`);
  }
  expect(rule("html:not(.dark) .surface-glass-controls")).toContain(
    "--popover:",
  );
  expect(
    cardSurfaceStyle({ translucent: false, opacity: 60, blur: false }),
  ).toEqual({ className: "bg-card" });
});
