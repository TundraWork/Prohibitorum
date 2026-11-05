import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  type RegisteredRouter,
  type RouteComponent,
  RouterProvider,
  retainSearchParams,
} from "@tanstack/react-router";
import { render, waitFor } from "@testing-library/react";

import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { z } from "zod";
import {
  browserAccountsQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
import { readKnownAccounts } from "@/app/account-storage";
import {
  afterSignIn,
  announceAccountsChanged,
  listenForAccountChanges,
  reconcileTabAccount,
  resetAccountsChannel,
  useAccountSync,
} from "@/app/account-sync";
import { loginSearch } from "@/app/login-search";
import { createQueryClient } from "@/app/query-client";
import { optionalSearchText, searchSerialization } from "@/app/search-params";
import {
  type BrowserAccount,
  browserAccountCountAtom,
  pinTabAccount,
  tabAccountAtom,
} from "@/app/tab-account";
import { store } from "@/i18n";
import { testSession } from "@/test/app";

/** Stands in for the browser's channel: what is posted, and a way to deliver. */
class FakeChannel {
  static instances: FakeChannel[] = [];
  readonly posted: unknown[] = [];
  readonly listeners = new Set<(event: MessageEvent) => void>();
  closed = false;
  constructor(readonly name: string) {
    FakeChannel.instances.push(this);
  }
  postMessage(message: unknown) {
    this.posted.push(message);
  }
  addEventListener(_type: "message", listener: (event: MessageEvent) => void) {
    this.listeners.add(listener);
  }
  removeEventListener(
    _type: "message",
    listener: (event: MessageEvent) => void,
  ) {
    this.listeners.delete(listener);
  }
  close() {
    this.closed = true;
  }
  deliver(data: unknown) {
    for (const listener of this.listeners) {
      listener(new MessageEvent("message", { data }));
    }
  }
}

function account(accountId: number, u: string): BrowserAccount {
  return {
    accountId,
    u,
    username: `user${accountId}`,
    displayName: `User ${accountId}`,
  };
}

const lin = account(12, "3fa9");
const ops = account(40, "b071");

let queryClient: QueryClient;

beforeEach(() => {
  resetAccountsChannel();
  FakeChannel.instances = [];
  vi.stubGlobal("BroadcastChannel", FakeChannel);
  queryClient = createQueryClient(() => undefined);
});

afterEach(() => {
  resetAccountsChannel();
  queryClient.clear();
  vi.unstubAllGlobals();
});

function testRouter(path: string, Layout: RouteComponent = Outlet) {
  const root = createRootRoute({
    validateSearch: z.object({ u: optionalSearchText() }),
    search: { middlewares: [retainSearchParams(["u"])] },
    component: Layout,
  });
  const page = (at: string) =>
    createRoute({
      getParentRoute: () => root,
      path: at,
      component: () => <h1>{at}</h1>,
    });
  const login = createRoute({
    getParentRoute: () => root,
    path: "/login",
    validateSearch: loginSearch,
    component: () => <h1>/login</h1>,
  });
  const router = createRouter({
    ...searchSerialization,
    routeTree: root.addChildren([page("/"), page("/profile"), login]),
    history: createMemoryHistory({ initialEntries: [path] }),
  });
  render(
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>,
  );
  // The app's functions take the registered router; this one has the routes
  // they use.
  return router as unknown as RegisteredRouter;
}

async function settled(router: RegisteredRouter) {
  await waitFor(() => expect(router.state.status).toBe("idle"));
  return router;
}

describe("the accounts channel", () => {
  it("tells the other tabs that the accounts changed", () => {
    announceAccountsChanged();
    const [channel] = FakeChannel.instances;
    expect(channel?.name).toBe("prohibitorum-accounts");
    expect(channel?.posted).toEqual([{ type: "changed" }]);
  });

  it("hears a change, ignores anything else, and stops when asked", () => {
    const onChange = vi.fn();
    const stop = listenForAccountChanges(onChange);
    const [channel] = FakeChannel.instances;
    channel?.deliver({ type: "changed" });
    channel?.deliver({ type: "other" });
    channel?.deliver(null);
    expect(onChange).toHaveBeenCalledOnce();
    stop();
    channel?.deliver({ type: "changed" });
    expect(onChange).toHaveBeenCalledOnce();
  });

  it("sends and hears on one channel, so a tab does not hear itself", () => {
    listenForAccountChanges(() => undefined);
    announceAccountsChanged();
    expect(FakeChannel.instances).toHaveLength(1);
  });

  it("does nothing where the browser has no channel", () => {
    resetAccountsChannel();
    vi.stubGlobal("BroadcastChannel", undefined);
    expect(() => announceAccountsChanged()).not.toThrow();
    expect(listenForAccountChanges(() => undefined)).toBeTypeOf("function");
  });

  it("reads the list again when another tab says it changed", async () => {
    let accounts = [lin];
    const fetch = vi.fn(async () => Response.json({ limit: 10, accounts }));
    vi.stubGlobal("fetch", fetch);
    pinTabAccount(lin, 1);
    function Synced() {
      useAccountSync();
      return <Outlet />;
    }
    const router = await settled(testRouter("/profile", Synced));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    accounts = [lin, ops];
    FakeChannel.instances[0]?.deliver({ type: "changed" });
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    // Another account arrived, so the address now names this tab's.
    await waitFor(() =>
      expect(router.state.location.search).toMatchObject({ u: "3fa9" }),
    );
    expect(store.get(browserAccountCountAtom)).toBe(2);
  });
});

describe("bringing the tab in line with the list", () => {
  it("drops u once the tab's account is the only one left", async () => {
    pinTabAccount(lin, 2);
    const router = await settled(testRouter("/profile?u=3fa9"));
    await reconcileTabAccount(router, queryClient, [lin]);
    expect(router.state.location.pathname).toBe("/profile");
    expect(router.state.location.searchStr).toBe("");
    expect(store.get(tabAccountAtom)?.accountId).toBe(12);
  });

  it("adds u once another account joins the browser", async () => {
    pinTabAccount(lin, 1);
    const router = await settled(testRouter("/profile"));
    await reconcileTabAccount(router, queryClient, [lin, ops]);
    expect(router.state.location.searchStr).toBe("?u=3fa9");
  });

  it("sends a tab whose account signed out to sign in as it again", async () => {
    pinTabAccount(ops, 2);
    await queryClient.prefetchQuery({
      ...sessionQueryOptions(),
      queryFn: () => testSession,
    });
    const router = await settled(testRouter("/profile?u=b071"));
    await reconcileTabAccount(router, queryClient, [lin]);
    await settled(router);
    expect(router.state.location.pathname).toBe("/login");
    const search = new URLSearchParams(router.state.location.searchStr);
    expect(search.get("return_to")).toBe("/profile");
    expect(search.get("login_hint")).toBe("user40");
    expect(search.has("u")).toBe(false);
    expect(store.get(tabAccountAtom)).toBeNull();
    expect(
      queryClient.getQueryData(sessionQueryOptions().queryKey),
    ).toBeUndefined();
  });

  it("leaves the sign-in page alone when an account signs out", async () => {
    pinTabAccount(ops, 2);
    const router = await settled(testRouter("/login"));
    await reconcileTabAccount(router, queryClient, [lin]);
    expect(router.state.location.pathname).toBe("/login");
    expect(store.get(browserAccountCountAtom)).toBe(1);
  });
});

describe("after this tab signs an account in", () => {
  it("reads the list, takes the new account, keeps its username and tells the others", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => Response.json({ limit: 10, accounts: [lin, ops] })),
    );
    pinTabAccount(lin, 1);
    await afterSignIn(queryClient, { accountId: 40, u: "b071" });
    expect(store.get(tabAccountAtom)).toEqual({
      accountId: 40,
      u: "b071",
      username: "user40",
    });
    expect(store.get(browserAccountCountAtom)).toBe(2);
    expect(readKnownAccounts()).toEqual({ b071: "user40" });
    expect(
      queryClient.getQueryData(browserAccountsQueryOptions().queryKey),
    ).toEqual({ limit: 10, accounts: [lin, ops] });
    expect(FakeChannel.instances[0]?.posted).toEqual([{ type: "changed" }]);
  });
});
