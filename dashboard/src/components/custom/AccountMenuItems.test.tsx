import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import { readKnownAccounts, rememberKnownAccount } from "@/app/account-storage";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  browserAccount,
  type FakeApi,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";

const alice = browserAccount(testSession, "a11c");
const bob = browserAccount(
  { id: 2, username: "bob", displayName: "Bob" },
  "b0b0",
);
const carol = browserAccount(
  { id: 3, username: "carol", displayName: "Carol" },
  "c4r0",
);

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

/**
 * The launcher with `accounts` on the browser; signing out removes the
 * account the request named (or the first), as the server does.
 */
function launcher(
  initial: ReturnType<typeof browserAccount>[],
  apps: components["schemas"]["LaunchpadApp"][] = [],
): FakeApi {
  let accounts = initial;
  return fakeApi({
    ...publicApi(testSession),
    "GET /api/prohibitorum/me": () =>
      accounts.length > 0
        ? Response.json(testSession)
        : apiError("no_session", 401),
    "GET /api/prohibitorum/auth/accounts": () =>
      Response.json({ limit: 10, accounts }),
    "GET /api/prohibitorum/me/apps": () => Response.json(apps),
    "GET /api/prohibitorum/me/consent": () =>
      Response.json(
        apps.map((app) => ({
          kind: app.kind,
          clientId: app.id,
          name: app.name,
          grantedAt: new Date().toISOString(),
          scopes: [],
        })),
      ),
    "POST /api/prohibitorum/auth/logout": (request) => {
      const named = Number(request.headers.get("X-Prohibitorum-Account"));
      const gone = named || accounts[0]?.accountId;
      accounts = accounts.filter((account) => account.accountId !== gone);
      return new Response(null, { status: 204 });
    },
    "POST /api/prohibitorum/auth/logout-all": () => {
      accounts = [];
      return new Response(null, { status: 204 });
    },
  });
}

async function openMenu() {
  const user = userEvent.setup();
  await user.click(await screen.findByRole("button", { name: "Your account" }));
  return { user, menu: await screen.findByRole("menu") };
}

describe("the avatar menu's accounts", () => {
  it("offers only Add account, and one sign-out, with one account", async () => {
    launcher([alice]);
    renderApp("/", queryClient);
    const { menu } = await openMenu();
    expect(
      within(menu).getByRole("menuitem", { name: "Add account" }),
    ).toBeVisible();
    expect(within(menu).queryByText("Other accounts")).not.toBeInTheDocument();
    expect(
      within(menu).getByRole("menuitem", { name: "Sign out" }),
    ).toBeVisible();
    expect(
      within(menu).queryByRole("menuitem", {
        name: "Sign out of all accounts",
      }),
    ).not.toBeInTheDocument();
  });

  it("lists the other accounts, each opening in a new tab", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    launcher([alice, bob, carol]);
    renderApp("/?u=b0b0", queryClient);
    const { user, menu } = await openMenu();
    expect(within(menu).getByText("Other accounts")).toBeVisible();
    expect(within(menu).getByRole("menuitem", { name: "Alice" })).toBeVisible();
    expect(within(menu).getByRole("menuitem", { name: "Carol" })).toBeVisible();
    expect(
      within(menu).queryByRole("menuitem", { name: "Bob" }),
    ).not.toBeInTheDocument();
    await user.click(within(menu).getByRole("menuitem", { name: "Carol" }));
    expect(open).toHaveBeenCalledWith("/?u=c4r0", "_blank", "noopener");
  });

  it("opens the sign-in page in a new tab to add an account", async () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    launcher([alice, bob]);
    renderApp("/", queryClient);
    const { user, menu } = await openMenu();
    await user.click(
      within(menu).getByRole("menuitem", { name: "Add account" }),
    );
    expect(open).toHaveBeenCalledWith("/login?add=1", "_blank", "noopener");
  });

  it("signs out this tab's account only, and opens home as the one left", async () => {
    rememberKnownAccount("b0b0", "bob");
    rememberKnownAccount("a11c", "alice");
    const api = launcher([alice, bob]);
    const router = renderApp("/?u=b0b0", queryClient);
    const { user, menu } = await openMenu();
    await user.click(within(menu).getByRole("menuitem", { name: "Sign out" }));
    await waitFor(() =>
      expect(api.sent("POST", "/api/prohibitorum/auth/logout")).toHaveLength(1),
    );
    expect(
      api
        .sent("POST", "/api/prohibitorum/auth/logout")[0]
        ?.headers.get("X-Prohibitorum-Account"),
    ).toBe("2");
    await waitFor(() => expect(router.state.location.href).toBe("/"));
    expect(readKnownAccounts()).toEqual({ a11c: "alice" });
  });

  it("signs every account out and goes to the sign-in page", async () => {
    rememberKnownAccount("a11c", "alice");
    const api = launcher([alice, bob]);
    const router = renderApp("/", queryClient);
    const { user, menu } = await openMenu();
    await user.click(
      within(menu).getByRole("menuitem", { name: "Sign out of all accounts" }),
    );
    await waitFor(() => expect(router.state.location.pathname).toBe("/login"));
    expect(api.sent("POST", "/api/prohibitorum/auth/logout-all")).toHaveLength(
      1,
    );
    expect(readKnownAccounts()).toEqual({});
  });
});

describe("the console's avatar menu", () => {
  it("carries the same accounts, above the account's own pages", async () => {
    launcher([alice, bob]);
    renderApp("/security?u=a11c", queryClient);
    const user = userEvent.setup();
    await user.click(await screen.findByRole("button", { name: /Alice/ }));
    const menu = await screen.findByRole("menu");
    const items = within(menu).getAllByRole("menuitem");
    const at = (name: string) =>
      items.indexOf(within(menu).getByRole("menuitem", { name }));
    expect(at("Bob")).toBeLessThan(at("Profile"));
    expect(at("Add account")).toBeLessThan(at("Profile"));
    expect(
      within(menu).getByRole("menuitem", { name: "Add account" }),
    ).toBeVisible();
    expect(
      within(menu).getByRole("menuitem", { name: "Sign out of all accounts" }),
    ).toBeVisible();
  });
});

describe("opening a SAML service from the launcher", () => {
  const expenses: components["schemas"]["LaunchpadApp"] = {
    kind: "saml",
    id: "7",
    name: "Expenses",
    launchUrl: "/saml/sso/init?sp=https%3A%2F%2Fexpenses.example.test",
  };

  it("binds the sign-in to this tab's account when there are several", async () => {
    const tab = { opener: {}, location: { href: "about:blank" } };
    const open = vi
      .spyOn(window, "open")
      .mockReturnValue(tab as unknown as Window);
    const api = launcher([alice, bob], [expenses]);
    api.routes["POST /api/prohibitorum/me/apps/saml-launch"] = () =>
      Response.json({
        redirect: "/saml/sso/init?sp=x&account_choice=n-1",
      });
    renderApp("/?u=b0b0", queryClient);
    const user = userEvent.setup();
    await user.click(
      await screen.findByRole("link", { name: "Open Expenses in a new tab" }),
    );
    expect(open).toHaveBeenCalledWith("about:blank", "_blank");
    await waitFor(() =>
      expect(tab.location.href).toBe("/saml/sso/init?sp=x&account_choice=n-1"),
    );
    expect(tab.opener).toBeNull();
    const [launch] = api.sent("POST", "/api/prohibitorum/me/apps/saml-launch");
    expect(launch?.url.searchParams.get("sp")).toBe(
      "https://expenses.example.test",
    );
    expect(launch?.headers.get("X-Prohibitorum-Account")).toBe("2");
  });
});
