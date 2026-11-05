import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { loadDocument } from "@/app/load-document";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import {
  apiError,
  browserAccount,
  fakeApi,
  publicApi,
  renderApp,
  testSession,
} from "@/test/app";

vi.mock("@/app/load-document", () => ({ loadDocument: vi.fn() }));

const logoutPath = "/api/prohibitorum/auth/logout";

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
  vi.mocked(loadDocument).mockReset();
});

function signOutApi() {
  return fakeApi({
    ...publicApi(testSession),
    [`POST ${logoutPath}`]: () => new Response(null, { status: 204 }),
  });
}

describe("the sign-out confirmation", () => {
  it("signs out, then returns through sso-logout with the address it was given", async () => {
    const api = signOutApi();
    let signedOutFirst = false;
    vi.mocked(loadDocument).mockImplementation(() => {
      signedOutFirst = api.sent("POST", logoutPath).length === 1;
    });
    const user = userEvent.setup();
    renderApp("/sign-out?rd=https%3A%2F%2Fapp.example.test%2F", queryClient);
    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(loadDocument).toHaveBeenCalledOnce());
    expect(loadDocument).toHaveBeenCalledWith(
      "/api/prohibitorum/forward-auth/sso-logout?rd=https%3A%2F%2Fapp.example.test%2F",
    );
    expect(signedOutFirst).toBe(true);
  });

  it("returns through sso-logout without an address when it was given none", async () => {
    signOutApi();
    const user = userEvent.setup();
    renderApp("/sign-out", queryClient);
    await user.click(await screen.findByRole("button", { name: "Sign out" }));
    await waitFor(() => expect(loadDocument).toHaveBeenCalledOnce());
    expect(loadDocument).toHaveBeenCalledWith(
      "/api/prohibitorum/forward-auth/sso-logout",
    );
  });

  it("stays on the page when signing out fails", async () => {
    const api = signOutApi();
    api.routes[`POST ${logoutPath}`] = () => apiError("server_error", 500);
    const notifyError = vi.fn();
    queryClient = createQueryClient(notifyError);
    const user = userEvent.setup();
    renderApp("/sign-out?rd=https%3A%2F%2Fapp.example.test%2F", queryClient);
    const signOut = await screen.findByRole("button", { name: "Sign out" });
    await user.click(signOut);
    // The failure has been reported: the request is over, not yet to start.
    await waitFor(() => expect(notifyError).toHaveBeenCalledOnce());
    expect(api.sent("POST", logoutPath)).toHaveLength(1);
    await waitFor(() =>
      expect(screen.getByRole("button", { name: "Cancel" })).toBeEnabled(),
    );
    expect(signOut).not.toHaveAttribute("aria-busy");
    expect(loadDocument).not.toHaveBeenCalled();
  });

  it("goes home when cancelled with no page to go back to", async () => {
    signOutApi();
    vi.spyOn(window.history, "length", "get").mockReturnValue(1);
    const back = vi
      .spyOn(window.history, "back")
      .mockImplementation(() => undefined);
    const user = userEvent.setup();
    const router = renderApp(
      "/sign-out?rd=https%3A%2F%2Fapp.example.test%2F",
      queryClient,
    );
    await user.click(await screen.findByRole("button", { name: "Cancel" }));
    await waitFor(() => expect(router.state.location.pathname).toBe("/"));
    expect(back).not.toHaveBeenCalled();
    expect(loadDocument).not.toHaveBeenCalled();
  });

  it("goes back without signing out when cancelled", async () => {
    const api = signOutApi();
    vi.spyOn(window.history, "length", "get").mockReturnValue(2);
    const back = vi
      .spyOn(window.history, "back")
      .mockImplementation(() => undefined);
    const user = userEvent.setup();
    renderApp("/sign-out?rd=https%3A%2F%2Fapp.example.test%2F", queryClient);
    await user.click(await screen.findByRole("button", { name: "Cancel" }));
    expect(back).toHaveBeenCalledOnce();
    expect(api.sent("POST", logoutPath)).toHaveLength(0);
    expect(loadDocument).not.toHaveBeenCalled();
  });
});

describe("the sign-out confirmation with several accounts", () => {
  const alice = browserAccount(testSession, "a11c");
  const bob = browserAccount(
    { id: 2, username: "bob", displayName: "Bob" },
    "b0b0",
  );

  function severalApi() {
    let accounts = [alice, bob];
    return fakeApi({
      ...publicApi(testSession),
      "GET /api/prohibitorum/auth/accounts": () =>
        Response.json({ limit: 10, accounts }),
      [`POST ${logoutPath}`]: (request) => {
        const named = Number(request.headers.get("X-Prohibitorum-Account"));
        accounts = accounts.filter((account) => account.accountId !== named);
        return new Response(null, { status: 204 });
      },
    });
  }

  it("signs each account out on its own, then returns through sso-logout after the last", async () => {
    const api = severalApi();
    const user = userEvent.setup();
    renderApp("/sign-out?rd=https%3A%2F%2Fapp.example.test%2F", queryClient);
    expect(
      await screen.findByText(
        "app.example.test asked to sign out. 2 accounts are signed in on this browser.",
      ),
    ).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Sign out bob" }));
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "Sign out bob" }),
      ).not.toBeInTheDocument(),
    );
    expect(
      screen.getByText(
        "app.example.test asked to sign out. 1 account is signed in on this browser.",
      ),
    ).toBeVisible();
    expect(loadDocument).not.toHaveBeenCalled();

    await user.click(screen.getByRole("button", { name: "Sign out alice" }));
    await waitFor(() => expect(loadDocument).toHaveBeenCalledOnce());
    expect(loadDocument).toHaveBeenCalledWith(
      "/api/prohibitorum/forward-auth/sso-logout?rd=https%3A%2F%2Fapp.example.test%2F",
    );
    expect(
      api
        .sent("POST", logoutPath)
        .map((request) => request.headers.get("X-Prohibitorum-Account")),
    ).toEqual(["2", "1"]);
  });

  it("holds the other buttons while one account signs out", async () => {
    const api = severalApi();
    api.routes[`POST ${logoutPath}`] = () => new Promise<Response>(() => {});
    const user = userEvent.setup();
    renderApp("/sign-out?rd=https%3A%2F%2Fapp.example.test%2F", queryClient);
    const bobOut = await screen.findByRole("button", { name: "Sign out bob" });
    await user.click(bobOut);
    await waitFor(() => expect(bobOut).toHaveAttribute("aria-busy", "true"));
    expect(
      screen.getByRole("button", { name: "Sign out alice" }),
    ).toBeDisabled();
    expect(
      screen.getByRole("button", { name: "Return to app.example.test" }),
    ).toBeDisabled();
  });

  it("returns to the application without signing anything out", async () => {
    const api = severalApi();
    const user = userEvent.setup();
    renderApp("/sign-out?rd=https%3A%2F%2Fapp.example.test%2F", queryClient);
    await user.click(
      await screen.findByRole("button", { name: "Return to app.example.test" }),
    );
    expect(loadDocument).toHaveBeenCalledWith(
      "/api/prohibitorum/forward-auth/sso-logout?rd=https%3A%2F%2Fapp.example.test%2F&done=1",
    );
    expect(api.sent("POST", logoutPath)).toHaveLength(0);
  });
});
