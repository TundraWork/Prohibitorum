import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { type ReactNode, useState } from "react";
import { ApiError } from "@/api/errors";
import { chooseAccountMutationOptions } from "@/api/mutations";
import {
  accountChooserQueryOptions,
  browserAccountsQueryOptions,
  refreshBrowserAccounts,
} from "@/api/queries";
import type { AccountChooserAccount } from "@/api/raw-paths";
import { readLastUsed, sameRef, writeLastUsed } from "@/app/account-storage";
import { followRedirect, sitePathOf } from "@/app/redirect";
import { withRouterSkipLoading } from "@/app/router";
import { withoutU, withU } from "@/app/tab-account";
import { usePageRestore } from "@/app/use-page-restore";
import {
  AccountList,
  AccountRow,
  OtherAccountRow,
} from "@/components/custom/AccountRow";
import { AppStrip } from "@/components/custom/AppStrip";
import { PublicStep } from "@/components/custom/PublicStep";
import { Route } from "@/routes/_public.choose-account";

/** The row the reader pressed: an account by ID, or "another account". */
type Choice = number | "other";

/**
 * Pressing a row continues at once; there is no separate button. Until the
 * page has gone, the pressed row carries the spinner and every row is held
 * (mockup A2); a page brought back from the back-forward cache lets go.
 */
function useChoice() {
  const [chosen, setChosen] = useState<Choice>();
  usePageRestore(() => setChosen(undefined));
  return { chosen, setChosen };
}

export function ChooseAccountPage() {
  const { ticket, return_to: returnTo } = Route.useSearch();
  if (ticket) return <TicketChooser ticket={ticket} />;
  return <AddressChooser returnTo={sitePathOf(returnTo) ?? "/"} />;
}

function ChooserStep({
  strip,
  description,
  children,
}: {
  strip?: ReactNode;
  description?: ReactNode;
  children: ReactNode;
}) {
  return (
    <PublicStep
      strip={strip}
      title={<Trans id="choose_account.title">Choose an account</Trans>}
      description={description}
    >
      {children}
    </PublicStep>
  );
}

/**
 * A protocol sign-in with several accounts on the browser (mockups A and B).
 * Accounts the application's access policy turns away stay in the list,
 * marked and unpressable; "Use another account" is always open. "Last used"
 * only labels the account chosen for this application last time on this
 * browser: it neither reorders the list nor picks for the reader.
 */
function TicketChooser({ ticket }: { ticket: string }) {
  const { t } = useLingui();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { data: chooser } = useSuspenseQuery(
    accountChooserQueryOptions(ticket),
  );
  const choose = useMutation(chooseAccountMutationOptions());
  const { chosen, setChosen } = useChoice();
  const [lastUsed] = useState(() => readLastUsed(chooser.app.key));
  const app = chooser.app.name;
  const accounts = chooser.accounts ?? [];

  const pick = async (account: AccountChooserAccount) => {
    if (chosen !== undefined) return;
    setChosen(account.accountId);
    try {
      const { redirect } = await choose.mutateAsync({
        ticket,
        accountId: account.accountId,
      });
      writeLastUsed(chooser.app.key, account.u);
      // The row keeps its spinner until the next page has loaded: the consent
      // page opens in place, an application's address loads as a document.
      await withRouterSkipLoading(router, () =>
        followRedirect(router, redirect),
      );
    } catch (error) {
      setChosen(undefined);
      // The account signed out, or lost access, since the list was read:
      // the list is read again to show it as it is now.
      if (
        error instanceof ApiError &&
        (error.code === "account_not_signed_in" ||
          error.code === "app_access_denied")
      ) {
        await Promise.allSettled([
          queryClient.invalidateQueries({
            queryKey: accountChooserQueryOptions(ticket).queryKey,
          }),
          refreshBrowserAccounts(queryClient),
        ]);
      }
    }
  };

  const other = async () => {
    if (chosen !== undefined) return;
    setChosen("other");
    try {
      await followRedirect(router, chooser.otherAccountUrl);
    } catch {
      setChosen(undefined);
    }
  };

  return (
    <ChooserStep
      strip={
        <AppStrip
          lead={<Trans id="login.signing_in_to">You're signing in to</Trans>}
          name={app}
          iconUrl={chooser.app.iconUrl}
        />
      }
      description={
        <Trans id="choose_account.description.app">
          Continue to {app} as one of the accounts signed in on this browser.
        </Trans>
      }
    >
      <AccountList
        label={t({ id: "choose_account.list", message: "Accounts" })}
      >
        {accounts.map((account) => (
          <AccountRow
            key={account.accountId}
            account={account}
            onSelect={() => void pick(account)}
            unavailable={
              account.allowed ? undefined : (
                <Trans id="choose_account.denied">
                  Not allowed to use this app
                </Trans>
              )
            }
            tag={
              lastUsed !== undefined && sameRef(lastUsed, account.u) ? (
                <Trans id="choose_account.last_used">Last used</Trans>
              ) : undefined
            }
            pending={chosen === account.accountId}
            disabled={chosen !== undefined && chosen !== account.accountId}
          />
        ))}
        <OtherAccountRow
          onSelect={() => void other()}
          pending={chosen === "other"}
          disabled={chosen !== undefined && chosen !== "other"}
        >
          <Trans id="choose_account.other">Use another account</Trans>
        </OtherAccountRow>
      </AccountList>
    </ChooserStep>
  );
}

/**
 * A console address whose `u` matches more than one account on the browser,
 * which happens only when two accounts share a shortened `u`. The choice
 * opens the address again with that account's own `u`, which names it alone.
 */
function AddressChooser({ returnTo }: { returnTo: string }) {
  const { t } = useLingui();
  const router = useRouter();
  const { data } = useSuspenseQuery(browserAccountsQueryOptions());
  const { chosen, setChosen } = useChoice();

  const go = async (choice: Choice, target: string) => {
    if (chosen !== undefined) return;
    setChosen(choice);
    try {
      await followRedirect(router, target);
    } catch {
      setChosen(undefined);
    }
  };
  const signIn = `/login?${new URLSearchParams({ return_to: withoutU(returnTo) })}`;

  return (
    <ChooserStep
      description={
        <Trans id="choose_account.description.address">
          This address matches more than one account signed in on this browser.
        </Trans>
      }
    >
      <AccountList
        label={t({ id: "choose_account.list", message: "Accounts" })}
      >
        {data.accounts.map((account) => (
          <AccountRow
            key={account.accountId}
            account={account}
            onSelect={() =>
              void go(account.accountId, withU(returnTo, account.u))
            }
            pending={chosen === account.accountId}
            disabled={chosen !== undefined && chosen !== account.accountId}
          />
        ))}
        <OtherAccountRow
          onSelect={() => void go("other", signIn)}
          pending={chosen === "other"}
          disabled={chosen !== undefined && chosen !== "other"}
        >
          <Trans id="choose_account.other">Use another account</Trans>
        </OtherAccountRow>
      </AccountList>
    </ChooserStep>
  );
}
