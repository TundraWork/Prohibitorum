import { describe, expect, it } from "vitest";
import { readEnrollmentPreview } from "@/api/enrollment";
import { ApiError } from "@/api/errors";
import type { components } from "@/api/generated/schema";

type Raw = components["schemas"]["EnrollmentPreview"];

const gitlab = { slug: "gitlab", displayName: "GitLab", protocol: "oidc" };

function raw(overrides: Partial<Raw> = {}): Raw {
  return {
    intent: "invite",
    expiresAt: "2026-10-03T00:00:00Z",
    allowedMethods: ["passkey", "password_totp"],
    providers: [gitlab],
    ...overrides,
  };
}

function refused(value: Raw) {
  try {
    readEnrollmentPreview(value);
  } catch (error) {
    return error instanceof ApiError && error.kind === "invalid-response";
  }
  return false;
}

describe("readEnrollmentPreview", () => {
  it("passes a preview it can draw through, narrowed", () => {
    expect(
      readEnrollmentPreview(
        raw({
          intent: "reset",
          target: { username: "alice", displayName: "Alice" },
          expectedUpstreamIdpSlug: "gitlab",
          suggestedDisplayName: "Ali",
          username: "alice",
        }),
      ),
    ).toEqual({
      intent: "reset",
      username: "alice",
      target: { username: "alice", displayName: "Alice" },
      expiresAt: "2026-10-03T00:00:00Z",
      suggestedDisplayName: "Ali",
      allowedMethods: ["passkey", "password_totp"],
      expectedUpstreamIdpSlug: "gitlab",
      providers: [gitlab],
    });
  });

  it("reads a missing provider list as none", () => {
    const { providers, ...withoutProviders } = raw();
    expect(readEnrollmentPreview(withoutProviders).providers).toEqual([]);
    expect(readEnrollmentPreview(raw({ providers: null })).providers).toEqual(
      [],
    );
  });

  it("refuses an intent it does not know", () => {
    expect(refused(raw({ intent: "recover" }))).toBe(true);
  });

  it("refuses no methods, an empty list and a method it does not know", () => {
    expect(refused(raw({ allowedMethods: null }))).toBe(true);
    expect(refused(raw({ allowedMethods: [] }))).toBe(true);
    expect(refused(raw({ allowedMethods: ["passkey", "sms"] }))).toBe(true);
  });

  it("refuses a reset that does not name its account", () => {
    expect(refused(raw({ intent: "reset" }))).toBe(true);
  });

  it("refuses an invitation bound to a provider the list lacks", () => {
    expect(refused(raw({ expectedUpstreamIdpSlug: "github" }))).toBe(true);
    expect(
      refused(raw({ expectedUpstreamIdpSlug: "gitlab", providers: [] })),
    ).toBe(true);
  });
});
