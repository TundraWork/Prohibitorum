import { expect, it } from "vitest";
import {
  capsuleSurfaceStyle,
  cardSurfaceStyle,
  surfaceStyle,
} from "@/components/custom/login-appearance/surface-style";

it("leaves an opaque surface exactly as HeroUI draws it", () => {
  expect(surfaceStyle({ translucent: false, opacity: 40, blur: true })).toEqual(
    {
      className: "",
    },
  );
});

it("lowers a translucent surface's fill through one inline property", () => {
  const style = surfaceStyle({ translucent: true, opacity: 55, blur: false });
  expect(style.style).toEqual({ "--surface-alpha": "55%" });
  expect(style.className).toBe(
    "bg-[color-mix(in_oklab,var(--surface)_var(--surface-alpha),transparent)]",
  );
});

it("adds the blur only to a translucent surface with frosted glass on", () => {
  const style = surfaceStyle({ translucent: true, opacity: 80, blur: true });
  expect(style.className.split(" ")).toEqual(
    expect.arrayContaining(["backdrop-blur-xl", "backdrop-saturate-150"]),
  );
});

it("fills an opaque capsule with the surface colour", () => {
  expect(
    capsuleSurfaceStyle({ translucent: false, opacity: 70, blur: true }),
  ).toEqual({ className: "bg-surface" });
});

it("gives a translucent card's controls see-through fills, and an opaque card nothing", () => {
  const glass = cardSurfaceStyle({
    translucent: true,
    opacity: 60,
    blur: false,
  });
  expect(glass.className).toContain("[--field-background:");
  expect(glass.className).toContain("[--default-hover:");
  expect(glass.style).toEqual({ "--surface-alpha": "60%" });
  expect(
    cardSurfaceStyle({ translucent: false, opacity: 60, blur: false }),
  ).toEqual({ className: "" });
});
