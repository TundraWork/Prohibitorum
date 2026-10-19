import type { AppGroupView } from "@/api/raw-admin-paths";

/** A manual group that is sent to applications and that none of them uses. */
export function groupFixture(
  overrides: Partial<AppGroupView> = {},
): AppGroupView {
  return {
    id: 7,
    kind: "manual",
    slug: "staff",
    displayName: "Staff",
    description: "Everyone employed here",
    exposedToDownstream: true,
    applicationCount: 0,
    ...overrides,
  };
}

/** A rule group whose one condition matches accounts that sign in with a passkey. */
export function ruleGroupFixture(
  overrides: Partial<AppGroupView> = {},
): AppGroupView {
  return groupFixture({
    id: 8,
    kind: "rule",
    slug: "passkey-users",
    displayName: "Passkey users",
    description: "",
    rule: {
      version: 1,
      condition: {
        op: "all",
        children: [{ fact: "login_method", method: "passkey" }],
      },
    },
    ...overrides,
  });
}
