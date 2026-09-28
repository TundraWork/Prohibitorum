import { describe, expect, it } from "vitest";
import { isSitePath } from "@/app/redirect";

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
