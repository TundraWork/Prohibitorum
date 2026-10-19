import type { components } from "@/api/generated/schema";

export type Invitation = components["schemas"]["InvitationView"];

/** A pending invitation for an ordinary account with nothing fixed in advance. */
export function invitationFixture(
  overrides: Partial<Invitation> = {},
): Invitation {
  return {
    token: "tok-1",
    url: "https://test/enroll/tok-1",
    role: "user",
    createdAt: new Date(Date.now() - 2 * 24 * 3600 * 1000).toISOString(),
    expiresAt: new Date(Date.now() + 5 * 24 * 3600 * 1000).toISOString(),
    groupIds: [],
    groups: [],
    requireLocalCredential: false,
    ...overrides,
  };
}
