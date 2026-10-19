import { describe, expect, it } from "vitest";
import {
  hexToHsb,
  hsbToHex,
  withHue,
} from "@/pages/admin/settings/sign-in-page/color";

describe("hex and hue, saturation and brightness", () => {
  it.each([
    ["#ff0000", { h: 0, s: 100, b: 100 }],
    ["#00ff00", { h: 120, s: 100, b: 100 }],
    ["#0000ff", { h: 240, s: 100, b: 100 }],
    ["#000000", { h: 0, s: 0, b: 0 }],
    ["#ffffff", { h: 0, s: 0, b: 100 }],
    ["#1f6f8b", { h: 196, s: 78, b: 55 }],
  ])("reads %s", (hex, expected) => {
    expect(hexToHsb(hex)).toEqual(expected);
  });

  it.each(["#ff0000", "#00ff00", "#0000ff", "#000000", "#ffffff", "#808080"])(
    "writes %s back as it read it",
    (hex) => {
      expect(hsbToHex(hexToHsb(hex))).toBe(hex);
    },
  );

  it("writes lowercase #rrggbb", () => {
    expect(hsbToHex({ h: 196, s: 78, b: 55 })).toMatch(/^#[0-9a-f]{6}$/);
  });

  it("falls back to the theme's teal for text that is not a colour", () => {
    expect(hexToHsb("nonsense")).toEqual(hexToHsb("#1f6f8b"));
  });

  it("moves only the hue", () => {
    const moved = hexToHsb(withHue("#1f6f8b", 300));
    expect(moved.h).toBe(300);
    expect(moved.s).toBeGreaterThan(70);
    expect(moved.b).toBeGreaterThan(50);
  });
});
