import { expect, it } from "vitest";
import {
  capsuleSurfaceStyle,
  cardSurfaceStyle,
  surfaceStyle,
} from "@/components/custom/login-appearance/surface-style";

const heroUiTokens = [
  "--surface",
  "--field-background",
  "--default",
  "--separator",
  "--surface-secondary",
];

function expectNoHeroUiTokens(s: { className: string; style?: object }) {
  const text = JSON.stringify(s);
  for (const token of heroUiTokens) expect(text).not.toContain(token);
}

it("draws an opaque card with the background colour and nothing else", () => {
  expect(surfaceStyle({ translucent: false, opacity: 40, blur: true })).toEqual(
    { className: "bg-background" },
  );
});

it("fills a translucent card from the colour constants at the chosen opacity", () => {
  const s = surfaceStyle({ translucent: true, opacity: 50, blur: false });
  expect(s.style).toEqual({ "--card-alpha": "50%" });
  expect(s.className).toContain("var(--color-white)_var(--card-alpha)");
  expect(s.className).toContain("dark:bg-[");
  // The fill must not read --background, which the card redefines for its controls.
  expect(s.className).not.toMatch(/bg-\[[^\]]*var\(--background\)/);
  expect(s.className).not.toContain("bg-background");
  expectNoHeroUiTokens(s);
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

it("fills an opaque capsule with the background colour", () => {
  expect(
    capsuleSurfaceStyle({ translucent: false, opacity: 70, blur: true }),
  ).toEqual({ className: "bg-background" });
});

it("gives a translucent capsule the card's fill rule with its own settings", () => {
  const capsule = capsuleSurfaceStyle({
    translucent: true,
    opacity: 30,
    blur: false,
  });
  expect(capsule.style).toEqual({ "--card-alpha": "30%" });
  expect(capsule.className).toContain("var(--color-white)_var(--card-alpha)");
});

it("redefines the coss tokens controls read inside a translucent card, and an opaque card nothing", () => {
  const glass = cardSurfaceStyle({
    translucent: true,
    opacity: 60,
    blur: false,
  });
  for (const token of ["background", "input", "border", "muted", "accent"]) {
    expect(glass.className).toContain(`[--${token}:`);
    expect(glass.className).toContain(`dark:[--${token}:`);
  }
  expect(glass.style).toEqual({ "--card-alpha": "60%" });
  expectNoHeroUiTokens(glass);
  expect(
    cardSurfaceStyle({ translucent: false, opacity: 60, blur: false }),
  ).toEqual({ className: "bg-background" });
});
