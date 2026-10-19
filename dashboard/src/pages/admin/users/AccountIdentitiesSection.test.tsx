import { screen, within } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import type { components } from "@/api/generated/schema";
import { AccountIdentitiesSection } from "@/pages/admin/users/AccountIdentitiesSection";
import { accessHarness, refused } from "@/test/account-access";

type Identity = components["schemas"]["AccountIdentityView"];

const identity = (overrides: Partial<Identity>): Identity => ({
  id: 1,
  providerSlug: "corp",
  providerDisplayName: "Corp SSO",
  protocol: "oidc",
  subject: "00u1abcd2efg",
  linkedAt: "2026-01-01T00:00:00Z",
  data: {},
  ...overrides,
});

const path = "/api/prohibitorum/accounts/2/identities";

describe("the connected identities section", () => {
  const harness = accessHarness();
  const serve = (items: Identity[]) => (request: Request) =>
    request.url.endsWith(path) ? Response.json(items) : undefined;

  it("names each provider, its protocol in words and the subject, with nothing to unlink", async () => {
    harness.mount(
      <AccountIdentitiesSection accountId={2} />,
      serve([
        identity({}),
        identity({
          id: 2,
          providerDisplayName: "Steam",
          protocol: "steam",
          subject: "76561198000000000",
        }),
        identity({
          id: 3,
          providerDisplayName: "VRChat",
          protocol: "vrchat",
          subject: "usr_1",
        }),
        identity({
          id: 4,
          providerDisplayName: "Campus",
          protocol: "saml",
          subject: "persistent:abc",
        }),
      ]),
    );

    const list = await screen.findByRole("list", {
      name: "Connected identities",
    });
    const rows = within(list).getAllByRole("listitem");
    expect(rows).toHaveLength(4);
    expect(rows[0]).toHaveTextContent("Corp SSO");
    expect(rows[0]).toHaveTextContent("OpenID Connect");
    expect(rows[0]).toHaveTextContent("00u1abcd2efg");
    expect(rows[1]).toHaveTextContent("Steam");
    expect(rows[2]).toHaveTextContent("VRChat");
    expect(rows[3]).toHaveTextContent("SAML");
    expect(within(list).queryByRole("button")).toBeNull();
    expect(screen.getByText(/Administrators cannot unlink them/)).toBeVisible();
  });

  it("says so when the account has none", async () => {
    harness.mount(<AccountIdentitiesSection accountId={2} />, serve([]));

    expect(await screen.findByText("No connected identities")).toBeVisible();
  });

  it("tells a failed read once, in the section, and not as a toast", async () => {
    harness.mount(<AccountIdentitiesSection accountId={2} />, () => refused());

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(harness.notifyError).not.toHaveBeenCalled();
  });
});
