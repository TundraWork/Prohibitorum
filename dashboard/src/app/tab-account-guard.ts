import type { QueryClient } from "@tanstack/react-query";
import { type ParsedLocation, redirect } from "@tanstack/react-router";
import { browserAccountsQueryOptions } from "@/api/queries";
import { knownUsername, rememberKnownAccount } from "@/app/account-storage";
import {
  type BrowserAccount,
  isUnpinnedPath,
  pinTabAccount,
  resolveTabAccount,
  tabAccountAtom,
  withoutU,
} from "@/app/tab-account";
import { store } from "@/i18n";

/**
 * Settles which account the tab acts as before any page reads as it (design
 * 6.1), from the browser's accounts and the address's `u`: the address is
 * corrected in place, or the reader is sent to choose between the accounts a
 * shortened `u` matches, or to sign in as the account `u` named. A list that
 * cannot be read leaves the tab as it was, so the page still loads as the
 * server's default account.
 */
export async function settleTabAccount(
  queryClient: QueryClient,
  location: ParsedLocation,
): Promise<void> {
  let accounts: BrowserAccount[];
  try {
    ({ accounts } = await queryClient.ensureQueryData(
      browserAccountsQueryOptions(),
    ));
  } catch {
    return;
  }
  const search = location.search as Record<string, unknown>;
  const raw = search.u;
  const urlU = typeof raw === "string" && raw !== "" ? raw : undefined;
  const resolution = resolveTabAccount(
    accounts,
    urlU,
    store.get(tabAccountAtom)?.accountId,
  );
  // The value is written out explicitly, `undefined` included: the root's
  // `retainSearchParams` would otherwise carry the old one over.
  const replaceU = (u: string | undefined) =>
    redirect({
      to: location.pathname,
      search: { ...search, u },
      hash: location.hash,
      replace: true,
    } as unknown as Parameters<typeof redirect>[0]);

  if (isUnpinnedPath(location.pathname)) {
    // These pages keep a `u` that names one account and drop any other; they
    // never add one or send the reader elsewhere over it.
    if (
      resolution.kind === "pin" &&
      resolution.account !== null &&
      urlU !== undefined
    ) {
      pinTabAccount(resolution.account, accounts.length);
      return;
    }
    pinTabAccount(store.get(tabAccountAtom), accounts.length);
    if (raw !== undefined) throw replaceU(undefined);
    return;
  }

  switch (resolution.kind) {
    case "pin":
      pinTabAccount(resolution.account, accounts.length);
      if (resolution.account !== null) {
        rememberKnownAccount(resolution.account.u, resolution.account.username);
      }
      if (raw !== resolution.u) throw replaceU(resolution.u);
      return;
    case "strip":
      pinTabAccount(resolution.account, accounts.length);
      throw replaceU(undefined);
    case "choose":
      throw redirect({
        to: "/choose-account",
        search: { return_to: withoutU(location.href), u: undefined },
      });
    case "login":
      pinTabAccount(null, accounts.length);
      throw redirect({
        to: "/login",
        search: {
          return_to: withoutU(location.href),
          login_hint: knownUsername(urlU),
          u: undefined,
        },
      });
  }
}
