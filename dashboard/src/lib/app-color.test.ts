import { describe, expect, it } from "vitest";
import { appTint, hueForName, srgbToOklch } from "@/lib/app-color";

describe("an application's tint", () => {
  it("takes the hue of the icon's colour, at a calm chroma", () => {
    const blue = appTint("Anything", "#0082c9");
    expect(blue.hue).toBeGreaterThan(230);
    expect(blue.hue).toBeLessThan(260);
    expect(blue.chroma).toBeLessThanOrEqual(0.03);
  });

  it("falls back to a stable hue from the name, never a state hue", () => {
    expect(hueForName("Grafana")).toBe(hueForName("Grafana"));
    for (const name of ["Grafana", "Gitea", "Outline", "", "示例应用"]) {
      const hue = hueForName(name);
      // Clear of the green, amber and red the console keeps for state.
      expect(hue).toBeGreaterThanOrEqual(188);
      expect(hue).toBeLessThanOrEqual(312);
    }
    expect(appTint("Grafana", "not a colour").hue).toBe(hueForName("Grafana"));
  });

  it("reads only a six-digit hex colour", () => {
    expect(srgbToOklch("#fff")).toBeNull();
    expect(srgbToOklch("#ffffff")?.l).toBeCloseTo(1, 2);
  });
});
