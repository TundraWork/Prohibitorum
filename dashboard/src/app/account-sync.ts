import {
  type QueryClient,
  useQuery,
  useQueryClient,
} from "@tanstack/react-query";
import { type RegisteredRouter, useRouter } from "@tanstack/react-router";
import { useEffect } from "react";
import {
  browserAccountsQueryOptions,
  clearSessionQueries,
  refreshBrowserAccounts,
} from "@/api/queries";
import type { SignedInAccount } from "@/api/raw-paths";
import { rememberKnownAccount } from "@/app/account-storage";
import {
  type BrowserAccount,
  isUnpinnedPath,
  pinTabAccount,
  setTabAccountCheck,
  type TabAccount,
  tabAccountAtom,
  withoutU,
} from "@/app/tab-account";
import { store } from "@/i18n";

const channelName = "prohibitorum-accounts";

type AccountsMessage = { type: "changed" };

let channel: BroadcastChannel | null | undefined;

// One channel for sending and listening: a channel never hears its own
// messages, so a tab is not told about the change it just made.
function accountsChannel(): BroadcastChannel | null {
  if (channel !== undefined) return channel;
  if (typeof BroadcastChannel === "undefined") {
    channel = null;
    return channel;
  }
  channel = new BroadcastChannel(channelName);
  // Node's channel (the test runner's) would otherwise hold the process open.
  (channel as { unref?: () => void }).unref?.();
  return channel;
}

/** Tells the browser's other tabs that an account signed in or out here. */
export function announceAccountsChanged(): void {
  const message: AccountsMessage = { type: "changed" };
  accountsChannel()?.postMessage(message);
}

/** Runs `onChange` when another tab announces a change to the accounts. */
export function listenForAccountChanges(onChange: () => void): () => void {
  const current = accountsChannel();
  if (current === null) return () => undefined;
  const listener = (event: MessageEvent<unknown>) => {
    const data = event.data as Partial<AccountsMessage> | null;
    if (data?.type === "changed") onChange();
  };
  current.addEventListener("message", listener);
  return () => current.removeEventListener("message", listener);
}

/** Forgets the shared channel; for tests that replace `BroadcastChannel`. */
export function resetAccountsChannel(): void {
  channel?.close();
  channel = undefined;
}

let signingOut = false;

/**
 * The tab's account has gone from the browser: signed out here in another
 * tab, or from another application. The tab does not take up another
 * account; it signs in again as the same one, which comes back here.
 */
async function tabAccountSignedOut(
  router: RegisteredRouter,
  queryClient: QueryClient,
  account: TabAccount,
  count: number,
): Promise<void> {
  if (signingOut) return;
  signingOut = true;
  try {
    pinTabAccount(null, count);
    await clearSessionQueries(queryClient);
    const { href } = router.state.location;
    await router.navigate({
      to: "/login",
      search: {
        return_to: withoutU(href),
        login_hint: account.username,
        u: undefined,
      },
      replace: true,
    });
  } finally {
    signingOut = false;
  }
}

/**
 * Brings the tab in line with a fresh read of the browser's accounts. The
 * address carries `u` exactly while the browser has several accounts; a tab
 * whose account has gone signs it in again (see `tabAccountSignedOut`). Pages
 * that are not one account's (`isUnpinnedPath`) only take the new count.
 */
export async function reconcileTabAccount(
  router: RegisteredRouter,
  queryClient: QueryClient,
  accounts: readonly BrowserAccount[],
): Promise<void> {
  const pinned = store.get(tabAccountAtom);
  const current =
    pinned === null
      ? undefined
      : accounts.find((account) => account.accountId === pinned.accountId);
  const location = router.state.location;
  if (isUnpinnedPath(location.pathname) || pinned === null) {
    pinTabAccount(current ?? pinned, accounts.length);
    return;
  }
  if (current === undefined) {
    await tabAccountSignedOut(router, queryClient, pinned, accounts.length);
    return;
  }
  pinTabAccount(current, accounts.length);
  const raw = (location.search as Record<string, unknown>).u;
  const u =
    accounts.length > 1
      ? typeof raw === "string" && raw !== ""
        ? raw
        : current.u
      : undefined;
  if (raw === u) return;
  await router.navigate({
    to: location.pathname,
    search: { ...(location.search as Record<string, unknown>), u },
    hash: location.hash,
    replace: true,
  } as unknown as Parameters<RegisteredRouter["navigate"]>[0]);
}

/**
 * After this tab signs an account in: the list is read again, the tab acts
 * as the new account until its next page says otherwise, its username is
 * kept for the next sign-in, and the other tabs are told.
 */
export async function afterSignIn(
  queryClient: QueryClient,
  signedIn: SignedInAccount | undefined,
): Promise<void> {
  let accounts: BrowserAccount[] = [];
  try {
    ({ accounts } = await refreshBrowserAccounts(queryClient));
  } catch {
    // The next page reads the list again.
  }
  const account = accounts.find(
    (candidate) => candidate.accountId === signedIn?.accountId,
  );
  if (account !== undefined) {
    rememberKnownAccount(account.u, account.username);
    pinTabAccount(account, accounts.length);
  }
  announceAccountsChanged();
}

/**
 * Keeps the tab's account in step with the browser: reads the list again
 * when another tab announces a change and when the window regains focus
 * (the query's `refetchOnWindowFocus`), and checks it when a response says
 * the tab's account may be gone. Mounted once, by the app's root layout.
 */
export function useAccountSync(): void {
  const router = useRouter();
  const queryClient = useQueryClient();
  const { data } = useQuery(browserAccountsQueryOptions());

  useEffect(
    () =>
      listenForAccountChanges(() => {
        void queryClient.invalidateQueries({
          queryKey: browserAccountsQueryOptions().queryKey,
        });
      }),
    [queryClient],
  );

  useEffect(() => {
    // Every response of a burst may ask; one read answers them all.
    let checking = false;
    setTabAccountCheck(() => {
      if (checking) return;
      checking = true;
      void refreshBrowserAccounts(queryClient)
        .then(({ accounts }) =>
          reconcileTabAccount(router, queryClient, accounts),
        )
        .catch(() => undefined)
        .finally(() => {
          checking = false;
        });
    });
    return () => setTabAccountCheck(undefined);
  }, [router, queryClient]);

  useEffect(() => {
    if (data === undefined) return;
    void reconcileTabAccount(router, queryClient, data.accounts).catch(
      () => undefined,
    );
  }, [data, router, queryClient]);
}
