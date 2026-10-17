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
    "/x\t/y",
    "/x\n/y",
    "/x\r/y",
    "/%09/example.org",
    "/%0A/example.org",
    "/%0D/example.org",
    "/%0d/x",
    "/%0a/x",
    "/%2509/host",
    "/%250A/host",
    "/%252509/host",
    "/%25250A/host",
    "/%25255C/host",
    "/%00/host",
    "/%5C/host",
    "/%255C/host",
    "/x\\y",
    "/%u0000/x",
    "/\0/x",
  ])("refuses %s", (value) => {
    expect(isSitePath(value)).toBe(false);
  });

  it("keeps a percent-encoded letter of the alphabet", () => {
    expect(isSitePath("/%41")).toBe(true);
  });

  it("leaves the query out of the decision", () => {
    // A state value is data the endpoint receives, not a way off the site:
    // a literal percent, an encoded backslash or newline, and an incomplete
    // UTF-8 sequence all stay usable as query values.
    expect(isSitePath("/oauth/authorize?state=100%25")).toBe(true);
    expect(isSitePath("/oauth/authorize?state=a%25b")).toBe(true);
    expect(isSitePath("/oauth/authorize?state=x%5Cy")).toBe(true);
    expect(isSitePath("/oauth/authorize?state=x%0Ay")).toBe(true);
    expect(isSitePath("/oauth/authorize?state=%E4")).toBe(true);
    expect(isSitePath("/oauth/authorize?next=%2509//evil.example")).toBe(true);
  });
});

describe("sitePathOf", () => {
  it("resolves a same-origin address to its path", () => {
    const origin = window.location.origin;
    expect(sitePathOf(`${origin}/oauth/authorize?client_id=1`)).toBe(
      "/oauth/authorize?client_id=1",
    );
  });

  it("resolves a same-origin address with an empty path to the root", () => {
    expect(sitePathOf(window.location.origin)).toBe("/");
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
    "/%2509/host",
    "/%250A/host",
    "/%252509/host",
    "/%25250A/host",
    "/%25255C/host",
    "/%5C/host",
    "/%255C/host",
    "/%00/host",
    `${window.location.origin}/%09/example.org`,
    `${window.location.origin}/oauth%2509//evil.example`,
    `${window.location.origin}/oauth%255C%252509//evil.example`,
    "/oauth/authorize%2509//evil.example",
    "/oauth/authorize%25255C/host",
  ])("refuses %s", (value) => {
    expect(sitePathOf(value)).toBeUndefined();
  });

  it("keeps a return address whose query carries data characters", () => {
    expect(sitePathOf("/oauth/authorize?state=100%25")).toBe(
      "/oauth/authorize?state=100%25",
    );
    expect(sitePathOf("/oauth/authorize?state=%E4")).toBe(
      "/oauth/authorize?state=%E4",
    );
    expect(
      sitePathOf(`${window.location.origin}/oauth/authorize?state=a%25b`),
    ).toBe("/oauth/authorize?state=a%25b");
  });
});
