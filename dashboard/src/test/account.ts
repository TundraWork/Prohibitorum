import type { components } from "@/api/generated/schema";

export type Account = components["schemas"]["AccountView"];

/**
 * An enabled, ordinary account with a verified address, as the single-account
 * read returns it: without a last sign-in, which only list rows carry.
 */
export function accountFixture(overrides: Partial<Account> = {}): Account {
  return {
    id: 2,
    username: "bob",
    displayName: "Bob Stone",
    email: "bob@example.test",
    emailVerified: true,
    oidcSubject: "00000000-0000-4000-8000-000000000002",
    role: "user",
    disabled: false,
    attributes: { department: "ops" },
    matchingIdentities: [],
    createdAt: "2026-01-02T00:00:00Z",
    updatedAt: "2026-02-01T00:00:00Z",
    ...overrides,
  };
}
