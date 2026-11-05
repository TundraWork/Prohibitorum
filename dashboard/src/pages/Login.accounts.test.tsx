import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
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

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

/** The sign-in page with `initial` signed in and room for `limit`. */
function signInApi(
  initial: ReturnType<typeof browserAccount>[],
  limit = 10,
): FakeApi {
  let accounts = initial;
  return fakeApi({
    ...publicApi(accounts.length > 0 ? testSession : null),
    "GET /api/prohibitorum/auth/accounts": () =>
      Response.json({ limit, accounts }),
    "POST /api/prohibitorum/auth/logout": (request) => {
      const named = Number(request.headers.get("X-Prohibitorum-Account"));
      accounts = accounts.filter((account) => account.accountId !== named);
      return new Response(null, { status: 204 });
    },
  });
}

const username = () => screen.findByRole("textbox", { name: "Username" });

describe("the sign-in page with accounts on the browser", () => {
  it("fills in the username a link names", async () => {
    signInApi([]);
    renderApp("/login?login_hint=bob", queryClient);
    expect(await username()).toHaveValue("bob");
  });

  it("stays open to add an account while one is signed in", async () => {
    signInApi([alice]);
    const router = renderApp("/login?add=1", queryClient);
    expect(await username()).toBeVisible();
    expect(router.state.location.pathname).toBe("/login");
  });

  it("still sends a signed-in reader home without add", async () => {
    signInApi([alice]);
    const router = renderApp("/login", queryClient);
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
  });

  it("lists the accounts instead of the form at the limit, and brings the form back after one signs out", async () => {
    const api = signInApi([alice, bob], 2);
    const user = userEvent.setup();
    renderApp("/login?add=1", queryClient);
    expect(
      await screen.findByText(
        "2 accounts are signed in on this browser, the most it allows.",
      ),
    ).toBeVisible();
    expect(
      screen.queryByRole("textbox", { name: "Username" }),
    ).not.toBeInTheDocument();

    let release!: () => void;
    const held = new Promise<void>((resolve) => {
      release = resolve;
    });
    const signOut = api.routes["POST /api/prohibitorum/auth/logout"];
    api.routes["POST /api/prohibitorum/auth/logout"] = async (request) => {
      await held;
      return signOut?.(request) ?? new Response(null, { status: 204 });
    };
    const button = screen.getByRole("button", { name: "Sign out bob" });
    await user.click(button);
    await waitFor(() => expect(button).toHaveAttribute("aria-busy", "true"));
    expect(
      screen.getByRole("button", { name: "Sign out alice" }),
    ).toBeDisabled();
    release();

    expect(await username()).toBeVisible();
    const [sent] = api.sent("POST", "/api/prohibitorum/auth/logout");
    expect(sent?.headers.get("X-Prohibitorum-Account")).toBe("2");
  });

  it("keeps the form at the limit for an account already here signing in again", async () => {
    signInApi([alice, bob], 2);
    renderApp("/login?add=1&login_hint=Bob", queryClient);
    expect(await username()).toHaveValue("Bob");
    expect(screen.queryByText(/the most it allows/)).not.toBeInTheDocument();
  });
});
