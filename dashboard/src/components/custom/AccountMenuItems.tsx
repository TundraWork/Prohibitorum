import { Trans } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useAtomValue } from "jotai";
import { ArrowUpRight, LogOut, Plus } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import {
  logoutAllMutationOptions,
  logoutMutationOptions,
} from "@/api/mutations";
import {
  browserAccountsQueryOptions,
  refreshBrowserAccounts,
} from "@/api/queries";
import { clearKnownAccounts, forgetKnownAccount } from "@/app/account-storage";
import { announceAccountsChanged } from "@/app/account-sync";
import { pinTabAccount, tabAccountAtom } from "@/app/tab-account";
import { AccountAvatar } from "@/components/custom/AccountRow";
import { MenuGroup, MenuGroupLabel, MenuItem } from "@/components/ui/menu";
import { store } from "@/i18n";

/**
 * The avatar menus' account actions (mockups D and E), shared by the console
 * and the launcher: the browser's other accounts and "Add account", each in
 * a new tab, since a tab keeps its account for life; and signing out this
 * tab's account or every account.
 *
 * `pending` is the sign-out in flight, which the menu's trigger shows until
 * the page has changed.
 */
export function useAccountMenu() {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const tab = useAtomValue(tabAccountAtom, { store });
  const { data } = useQuery(browserAccountsQueryOptions());
  const logout = useMutation(logoutMutationOptions(queryClient));
  const logoutAll = useMutation(logoutAllMutationOptions(queryClient));
  const [pending, setPending] = useState<"one" | "all">();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const accounts = data?.accounts ?? [];
  const others = accounts.filter(
    (account) => account.accountId !== tab?.accountId,
  );

  // The tab lets go of its account before the list is read again, so the
  // read does not take the sign-out for one made elsewhere.
  const leave = async (remaining: () => Promise<number>) => {
    pinTabAccount(null, 0);
    queryClient.getMutationCache().clear();
    let left = 0;
    try {
      left = await remaining();
    } catch {
      // The next page reads the list again.
    }
    announceAccountsChanged();
    // With accounts left, home opens as the first of them; without, the
    // sign-in page.
    await navigate(
      left > 0
        ? { to: "/", search: { u: undefined } }
        : { to: "/login", search: { u: undefined } },
    );
  };

  const signOut = async () => {
    if (pending !== undefined) return;
    setPending("one");
    try {
      const signedOut = store.get(tabAccountAtom);
      await logout.mutateAsync();
      if (signedOut !== null) forgetKnownAccount(signedOut.u);
      await leave(
        async () => (await refreshBrowserAccounts(queryClient)).accounts.length,
      );
    } catch {
      // The toast has reported it.
    } finally {
      if (mounted.current) setPending(undefined);
    }
  };

  const signOutAll = async () => {
    if (pending !== undefined) return;
    setPending("all");
    try {
      await logoutAll.mutateAsync();
      clearKnownAccounts();
      await leave(async () => {
        await refreshBrowserAccounts(queryClient);
        return 0;
      });
    } catch {
      // The toast has reported it.
    } finally {
      if (mounted.current) setPending(undefined);
    }
  };

  return { others, total: accounts.length, pending, signOut, signOutAll };
}

/**
 * The other accounts on the browser, then "Add account". With no other
 * account there is no heading, only "Add account".
 */
export function OtherAccountsMenuItems({
  others,
}: {
  others: ReturnType<typeof useAccountMenu>["others"];
}) {
  return (
    <MenuGroup>
      {others.length > 0 && (
        <MenuGroupLabel>
          <Trans id="account_menu.others">Other accounts</Trans>
        </MenuGroupLabel>
      )}
      {others.map((account) => (
        <MenuItem
          key={account.accountId}
          onClick={() =>
            window.open(
              `/?${new URLSearchParams({ u: account.u })}`,
              "_blank",
              "noopener",
            )
          }
        >
          <AccountAvatar account={account} className="size-6" />
          <span className="min-w-0 flex-1 truncate">
            {account.displayName || account.username}
          </span>
          <ArrowUpRight aria-hidden="true" className="opacity-60" />
        </MenuItem>
      ))}
      <MenuItem
        onClick={() => window.open("/login?add=1", "_blank", "noopener")}
      >
        <span
          aria-hidden="true"
          className="flex size-6 shrink-0 items-center justify-center rounded-full border border-dashed text-muted-foreground"
        >
          <Plus className="size-3.5" />
        </span>
        <span className="min-w-0 flex-1 truncate">
          <Trans id="account_menu.add">Add account</Trans>
        </span>
        <ArrowUpRight aria-hidden="true" className="opacity-60" />
      </MenuItem>
    </MenuGroup>
  );
}

/** "Sign out" for this tab's account, and "Sign out of all accounts" while there are several. */
export function SignOutMenuItems({
  menu,
}: {
  menu: ReturnType<typeof useAccountMenu>;
}) {
  const busy = menu.pending !== undefined;
  return (
    <>
      <MenuItem disabled={busy} onClick={() => void menu.signOut()}>
        <LogOut aria-hidden="true" />
        <Trans id="console.logout">Sign out</Trans>
      </MenuItem>
      {menu.total > 1 && (
        <MenuItem inset disabled={busy} onClick={() => void menu.signOutAll()}>
          <Trans id="account_menu.sign_out_all">Sign out of all accounts</Trans>
        </MenuItem>
      )}
    </>
  );
}
