import { describe, expect, it } from "vitest";
import { isSitePath, sitePathOf } from "@/app/redirect";

describe("isSitePath", () => {
  it("accepts a path on this site", () => {
    expect(isSitePath("/x")).toBe(true);
    expect(isSitePath("/apps?tab=1")).toBe(true);
  });

  it.each([
    undefined,
    "",
    "//x",
    "/\\x",
    "https://x",
    "x",
    "/%09/example.org",
    "/%0A/example.org",
    "/%0D/example.org",
    "/%0d/x",
    "/%0a/x",
    "/x\t/y",
    "/x\n/y",
    "/x\r/y",
  ])("refuses %s", (value) => {
    expect(isSitePath(value)).toBe(false);
  });

  it("keeps a percent-encoded letter of the alphabet", () => {
    expect(isSitePath("/%41")).toBe(true);
  });
});

describe("sitePathOf", () => {
  it("resolves a same-origin address to its path", () => {
    const origin = window.location.origin;
    expect(sitePathOf(`${origin}/oauth/authorize?client_id=1`)).toBe(
      "/oauth/authorize?client_id=1",
    );
  });

  it.each([
    undefined,
    "",
    "//x",
    "/\\x",
    "x",
    "https://evil.example/x",
    `${window.location.origin}//evil.example/pwn`,
    `${window.location.origin}/\\evil.example/pwn`,
    `https://user@${window.location.host}//evil.example/pwn`,
    "/%09/example.org",
    "/%0A/example.org",
    `${window.location.origin}/%09/example.org`,
    `${window.location.origin}/oauth/authorize?next=%0A//evil.example`,
  ])("refuses %s", (value) => {
    expect(sitePathOf(value)).toBeUndefined();
  });

  it("resolves a same-origin address with an empty path to the root", () => {
    expect(sitePathOf(window.location.origin)).toBe("/");
  });
});
