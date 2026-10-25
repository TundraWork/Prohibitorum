import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it } from "vitest";
import type { components } from "@/api/generated/schema";
import { AccountTokensSection } from "@/pages/admin/users/AccountTokensSection";
import { accessHarness, refused } from "@/test/account-access";

type Token = components["schemas"]["PersonalAccessTokenView"];

const token = (overrides: Partial<Token>): Token => ({
  id: 5,
  name: "CI deploy",
  tokenHint: "prohibitorum_pat_…m001",
  access: "selected_apps",
  apps: [{ clientId: "wiki", displayName: "Wiki" }],
  createdAt: "2026-01-01T00:00:00Z",
  expiresAt: "2999-01-01T00:00:00Z",
  lastUsedAt: new Date(Date.now() - 86_400_000).toISOString(),
  ...overrides,
});

const path = "/api/prohibitorum/accounts/2/tokens";

describe("the access tokens section", () => {
  const harness = accessHarness();
  const serve =
    (items: Token[], extra?: (request: Request) => Response | undefined) =>
    (request: Request) => {
      const canned = extra?.(request);
      if (canned) return canned;
      if (request.method === "GET" && request.url.endsWith(path)) {
        return Response.json({ items, nextCursor: "" });
      }
      return undefined;
    };

  it("shows each token's name, hint, reach, expiry and last use", async () => {
    harness.mount(
      <AccountTokensSection accountId={2} />,
      serve([
        token({}),
        token({
          id: 6,
          name: "Old script",
          access: "sudo",
          apps: [],
          expiresAt: "2020-01-01T00:00:00Z",
          lastUsedAt: undefined,
        }),
        token({
          id: 7,
          name: "Forever",
          access: "all_apps",
          expiresAt: undefined,
        }),
      ]),
    );

    const list = await screen.findByRole("list", { name: "Access tokens" });
    const rows = within(list).getAllByRole("listitem");
    expect(rows[0]).toHaveTextContent("CI deploy");
    expect(rows[0]).toHaveTextContent("prohibitorum_pat_…m001");
    expect(rows[0]).toHaveTextContent("Wiki");
    expect(rows[0]).toHaveTextContent(/Expires/);
    expect(rows[0]).toHaveTextContent("Last used yesterday");
    expect(rows[1]).toHaveTextContent("Skips verifying your identity");
    expect(rows[1]).toHaveTextContent("Expired");
    expect(rows[1]).toHaveTextContent("Not used yet");
    expect(rows[2]).toHaveTextContent("Every application");
    expect(rows[2]).toHaveTextContent("Never expires");
  });

  it("says so when the account has none", async () => {
    harness.mount(<AccountTokensSection accountId={2} />, serve([]));

    expect(await screen.findByText("No access tokens")).toBeVisible();
  });

  it("tells a failed read once, in the section, and not as a toast", async () => {
    harness.mount(<AccountTokensSection accountId={2} />, () => refused());

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(harness.notifyError).not.toHaveBeenCalled();
  });

  it("revokes a token only after the confirmation, then reads the list again", async () => {
    const user = userEvent.setup();
    let reads = 0;
    harness.mount(
      <AccountTokensSection accountId={2} />,
      serve([token({})], (request) => {
        if (request.method === "GET") reads += 1;
        return undefined;
      }),
    );

    await user.click(
      await screen.findByRole("button", { name: "Revoke CI deploy" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Revoke this token?",
    });
    // The confirmation names the token it is about to revoke.
    expect(within(dialog).getByText("CI deploy")).toBeVisible();
    expect(harness.sent("POST", "/tokens/revoke")).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Revoke token" }),
    );

    await waitFor(() =>
      expect(harness.sent("POST", "/tokens/revoke")).toHaveLength(1),
    );
    expect(await harness.sent("POST", "/tokens/revoke")[0]?.json()).toEqual({
      id: 5,
    });
    await waitFor(() => expect(reads).toBe(2));
  });

  it("reports a refused revoke once, inline, and closes the question", async () => {
    const user = userEvent.setup();
    harness.mount(
      <AccountTokensSection accountId={2} />,
      serve([token({})], (request) =>
        request.method === "POST" ? refused() : undefined,
      ),
    );

    await user.click(
      await screen.findByRole("button", { name: "Revoke CI deploy" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Revoke token" }),
    );

    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(harness.notifyError).not.toHaveBeenCalled();
    await waitFor(() => expect(screen.queryByRole("alertdialog")).toBeNull());
  });
});
