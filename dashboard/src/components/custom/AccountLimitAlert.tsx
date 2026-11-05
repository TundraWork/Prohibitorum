import { plural } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useState } from "react";
import { signOutAccountMutationOptions } from "@/api/mutations";
import {
  browserAccountsQueryOptions,
  refreshBrowserAccounts,
} from "@/api/queries";
import { announceAccountsChanged } from "@/app/account-sync";
import { AccountList, AccountRow } from "@/components/custom/AccountRow";
import { Button } from "@/components/custom/Button";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/**
 * Whether the browser holds as many accounts as it may, so another cannot
 * sign in. An account already here (`hint` names its username) can still sign
 * in again: it keeps its place rather than taking a new one.
 */
export function atAccountLimit(
  list: { limit: number; accounts: readonly { username: string }[] },
  hint?: string,
): boolean {
  if (list.accounts.length < list.limit) return false;
  const name = hint?.toLowerCase();
  return !list.accounts.some(
    (account) => account.username.toLowerCase() === name,
  );
}

/**
 * Stands in for a sign-in while the browser holds as many accounts as it
 * may (mockup C): the accounts, in sign-in order, each with its own sign-out.
 * Signing one out names it in the request, so no tab's own account is
 * touched by mistake; once the list is under the limit the page that drew
 * this shows its form again.
 */
export function AccountLimitAlert() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const { data } = useSuspenseQuery(browserAccountsQueryOptions());
  const signOut = useMutation(signOutAccountMutationOptions());
  const [pending, setPending] = useState<number>();
  // Drawn only at the limit, where the count and the limit are the same.
  const count = data.accounts.length;

  const signOutAccount = async (accountId: number) => {
    if (pending !== undefined) return;
    setPending(accountId);
    try {
      await signOut.mutateAsync(accountId);
      await refreshBrowserAccounts(queryClient);
      announceAccountsChanged();
    } catch {
      // The toast has reported it; the button comes back.
    } finally {
      setPending(undefined);
    }
  };

  // The list sits under the notice rather than in its indented column, which
  // left a narrow screen too little width for the names.
  return (
    <div data-slot="account-limit" className="flex flex-col gap-3">
      <SurfaceAlert status="warning" role="alert">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>
            {t({
              id: "account_limit.title",
              message: plural(count, {
                one: "# account is signed in on this browser, the most it allows.",
                other:
                  "# accounts are signed in on this browser, the most it allows.",
              }),
            })}
          </SurfaceAlert.Title>
        </SurfaceAlert.Content>
      </SurfaceAlert>
      <AccountList
        label={t({
          id: "account_limit.list",
          message: "Accounts on this browser",
        })}
      >
        {data.accounts.map((account) => (
          <AccountRow
            key={account.accountId}
            account={account}
            action={
              <SignOutAccountButton
                username={account.username}
                pending={pending === account.accountId}
                disabled={
                  pending !== undefined && pending !== account.accountId
                }
                onClick={() => void signOutAccount(account.accountId)}
              />
            }
          />
        ))}
      </AccountList>
    </div>
  );
}

/** One account's "Sign out", named for screen readers by the account it ends. */
export function SignOutAccountButton({
  username,
  pending,
  disabled,
  onClick,
}: {
  username: string;
  pending: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  const { t } = useLingui();
  return (
    <Button
      variant="outline"
      size="sm"
      className="shrink-0"
      pending={pending}
      disabled={disabled}
      aria-label={t({
        id: "account.sign_out.label",
        message: `Sign out ${username}`,
      })}
      onClick={onClick}
    >
      <Trans id="account.sign_out">Sign out</Trans>
    </Button>
  );
}
