import { describe, expect, it } from "vitest";
import { isSitePath, sitePathOf } from "@/app/redirect";

describe("isSitePath", () => {
  it("accepts a path on this site", () => {
    expect(isSitePath("/x")).toBe(true);
    expect(isSitePath("/apps?tab=1")).toBe(true);
  });

  it.each([undefined, "", "//x", "/\\x", "https://x", "x"])(
    "refuses %s",
    (value) => {
      expect(isSitePath(value)).toBe(false);
    },
  );
});

describe("sitePathOf", () => {
  it("resolves a same-origin address to its path", () => {
    const origin = window.location.origin;
    expect(sitePathOf(`${origin}/oauth/authorize?client_id=1`)).toBe(
      "/oauth/authorize?client_id=1",
    );
  });

  it.each([undefined, "", "//x", "/\\x", "x", "https://evil.example/x"])(
    "refuses %s",
    (value) => {
      expect(sitePathOf(value)).toBeUndefined();
    },
  );
});
