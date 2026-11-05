import type { QueryClient } from "@tanstack/react-query";
import { screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { rememberKnownAccount } from "@/app/account-storage";
import { createQueryClient } from "@/app/query-client";
import { followRedirect } from "@/app/redirect";
import { tabAccountAtom } from "@/app/tab-account";
import { i18n, store } from "@/i18n";
import {
  browserAccount,
  browserAccountsApi,
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

/** The launcher home with `accounts` signed in on the browser. */
function home(accounts: ReturnType<typeof browserAccount>[]): FakeApi {
  return fakeApi({
    ...publicApi(testSession),
    "GET /api/prohibitorum/auth/accounts": browserAccountsApi(accounts),
    "GET /api/prohibitorum/me/apps": () => Response.json([]),
    "GET /api/prohibitorum/me/consent": () => Response.json([]),
  });
}

function meHeaders(api: FakeApi) {
  return api
    .sent("GET", "/api/prohibitorum/me")
    .map((request) => request.headers.get("X-Prohibitorum-Account"));
}

describe("the tab's account from the address", () => {
  it("opens as the first account and names it in the address when there are several", async () => {
    const api = home([alice, bob]);
    const router = renderApp("/", queryClient);
    await waitFor(() =>
      expect(router.state.location.searchStr).toBe("?u=a11c"),
    );
    await waitFor(() => expect(meHeaders(api)).toContain("1"));
    expect(store.get(tabAccountAtom)?.accountId).toBe(1);
  });

  it("acts as the account u names, on every request", async () => {
    const api = home([alice, bob]);
    renderApp("/?u=b0b0", queryClient);
    await waitFor(() => expect(meHeaders(api)).toEqual(["2"]));
    expect(store.get(tabAccountAtom)?.username).toBe("bob");
  });

  it("drops u, and names no account, while the browser has one", async () => {
    const api = home([alice]);
    const router = renderApp("/?u=a11c", queryClient);
    await waitFor(() => expect(router.state.location.searchStr).toBe(""));
    await waitFor(() => expect(meHeaders(api)).toEqual([null]));
    expect(store.get(tabAccountAtom)?.accountId).toBe(1);
  });

  it("asks to sign in as the account u named when it is not here", async () => {
    rememberKnownAccount("c4r0", "carol");
    home([alice, bob]);
    const router = renderApp("/?u=c4r0", queryClient);
    expect(
      await screen.findByRole("textbox", { name: "Username" }),
    ).toHaveValue("carol");
    const search = new URLSearchParams(router.state.location.searchStr);
    expect(router.state.location.pathname).toBe("/login");
    expect(search.get("return_to")).toBe("/");
    expect(search.get("login_hint")).toBe("carol");
    expect(search.has("u")).toBe(false);
  });

  it("asks which account a shortened u means when it matches two", async () => {
    const first = browserAccount(testSession, "ab12cd34");
    const second = browserAccount(
      { id: 2, username: "bob", displayName: "Bob" },
      "ab12ef56",
    );
    home([first, second]);
    const router = renderApp("/profile?u=ab12", queryClient);
    expect(
      await screen.findByRole("heading", { name: "Choose an account" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/choose-account");
    expect(
      new URLSearchParams(router.state.location.searchStr).get("return_to"),
    ).toBe("/profile");
    expect(screen.getByRole("button", { name: /Bob/ })).toBeVisible();
  });

  it("keeps u on a link that replaces the whole search", async () => {
    home([alice, bob]);
    const router = renderApp("/?u=b0b0", queryClient);
    await waitFor(() => expect(router.state.status).toBe("idle"));
    await router.navigate({ to: "/devices", search: {} });
    expect(new URLSearchParams(router.state.location.searchStr).get("u")).toBe(
      "b0b0",
    );
  });

  it("follows a server redirect's own u, and keeps the tab's where it names none", async () => {
    home([alice, bob]);
    const router = renderApp("/?u=a11c", queryClient);
    await waitFor(() => expect(router.state.status).toBe("idle"));
    await followRedirect(router, "/devices?u=b0b0");
    expect(router.state.location.searchStr).toBe("?u=b0b0");
    await followRedirect(router, "/security");
    expect(router.state.location.searchStr).toBe("?u=b0b0");
  });
});
