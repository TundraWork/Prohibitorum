import { plural } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import {
  logoutMutationOptions,
  signOutAccountMutationOptions,
} from "@/api/mutations";
import {
  browserAccountsQueryOptions,
  clearSessionQueries,
  refreshBrowserAccounts,
} from "@/api/queries";
import { announceAccountsChanged } from "@/app/account-sync";
import { loadDocument } from "@/app/load-document";
import { usePageRestore } from "@/app/use-page-restore";
import { SignOutAccountButton } from "@/components/custom/AccountLimitAlert";
import { AccountList, AccountRow } from "@/components/custom/AccountRow";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";
import { Route } from "@/routes/_public.sign-out";

const ssoLogoutPath = "/api/prohibitorum/forward-auth/sso-logout";

function ssoLogout(rd: string | undefined, done = false): string {
  const query = new URLSearchParams();
  if (rd) query.set("rd", rd);
  if (done) query.set("done", "1");
  const search = query.toString();
  return search === "" ? ssoLogoutPath : `${ssoLogoutPath}?${search}`;
}

/** The host `rd` names, for the page to say who asked; nothing when unreadable. */
function hostOf(rd: string | undefined): string | undefined {
  if (!rd) return undefined;
  try {
    return new URL(rd).host || undefined;
  } catch {
    return undefined;
  }
}

/**
 * Asks before ending a session when an application's sign-out could have
 * come from anywhere. Signing out returns through sso-logout, which checks
 * `rd` and sends the browser back to the application; the page itself never
 * goes to `rd`, which anyone can write into the address.
 *
 * With several accounts on the browser the page cannot know which one the
 * application meant, so each is signed out on its own (mockup F).
 */
export function SignOutPage() {
  const { data } = useQuery(browserAccountsQueryOptions());
  // Decided once: signing accounts out one by one keeps the list in place
  // until the last one has gone.
  const [several] = useState(() => (data?.accounts.length ?? 0) > 1);
  return several ? <SignOutAccounts /> : <SignOutSingle />;
}

function useCancel() {
  const navigate = useNavigate();
  return async () => {
    if (window.history.length > 1) {
      window.history.back();
      return;
    }
    await navigate({ to: "/" });
  };
}

function SignOutSingle() {
  const { rd } = Route.useSearch();
  const { name } = useInstanceBranding();
  const queryClient = useQueryClient();
  const cancel = useCancel();
  const [leaving, setLeaving] = useState(false);
  const signOut = useMutation({
    ...logoutMutationOptions(queryClient),
    onSuccess: async () => {
      await clearSessionQueries(queryClient);
      announceAccountsChanged();
      setLeaving(true);
      loadDocument(ssoLogout(rd));
    },
  });
  usePageRestore(() => setLeaving(false));

  const busy = signOut.isPending || leaving;
  return (
    <PublicStep
      title={<Trans id="sign-out.title">Sign out of {name}?</Trans>}
      description={
        <Trans id="sign-out.description">
          An application asked to sign you out of {name}. If you did not choose
          to sign out, cancel.
        </Trans>
      }
      actions={
        <PublicStep.Actions layout="decision">
          <Button
            disabled={busy}
            onClick={() => void cancel()}
            variant="outline"
            size="lg"
          >
            <Trans id="sign-out.cancel">Cancel</Trans>
          </Button>
          <Button pending={busy} onClick={() => signOut.mutate()} size="lg">
            <Trans id="sign-out.confirm">Sign out</Trans>
          </Button>
        </PublicStep.Actions>
      }
    />
  );
}

/**
 * One row per account, each with its own "Sign out", which names that
 * account in the request; a row goes once its account has. After the last,
 * sso-logout finds no session and returns to the application. "Return to"
 * goes back without signing anything else out: sso-logout's `done=1` checks
 * `rd` and returns.
 */
function SignOutAccounts() {
  const { rd } = Route.useSearch();
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const cancel = useCancel();
  const { data } = useQuery(browserAccountsQueryOptions());
  const signOut = useMutation(signOutAccountMutationOptions());
  const [pending, setPending] = useState<number>();
  const [leaving, setLeaving] = useState<"back" | "done">();
  usePageRestore(() => setLeaving(undefined));
  const accounts = data?.accounts ?? [];
  const count = accounts.length;
  const host = hostOf(rd);
  const busy = pending !== undefined || leaving !== undefined;

  const signOutAccount = async (accountId: number) => {
    if (busy) return;
    setPending(accountId);
    try {
      await signOut.mutateAsync(accountId);
      const { accounts: left } = await refreshBrowserAccounts(queryClient);
      announceAccountsChanged();
      if (left.length === 0) {
        await clearSessionQueries(queryClient);
        setLeaving("done");
        loadDocument(ssoLogout(rd));
      }
    } catch {
      // The toast has reported it; the button comes back.
    } finally {
      setPending(undefined);
    }
  };

  const signedIn = t({
    id: "sign-out.accounts.count",
    message: plural(count, {
      one: "# account is signed in on this browser.",
      other: "# accounts are signed in on this browser.",
    }),
  });

  return (
    <PublicStep
      title={<Trans id="sign-out.accounts.title">Sign out</Trans>}
      description={
        host ? (
          <Trans id="sign-out.accounts.description.host">
            {host} asked to sign out. {signedIn}
          </Trans>
        ) : (
          <Trans id="sign-out.accounts.description">
            An application asked to sign out. {signedIn}
          </Trans>
        )
      }
      actions={
        <PublicStep.Actions layout="decision">
          <Button
            disabled={busy}
            onClick={() => void cancel()}
            variant="outline"
            size="lg"
          >
            <Trans id="sign-out.cancel">Cancel</Trans>
          </Button>
          <Button
            pending={leaving === "back"}
            disabled={busy && leaving !== "back"}
            onClick={() => {
              setLeaving("back");
              loadDocument(ssoLogout(rd, true));
            }}
            size="lg"
          >
            {host ? (
              <Trans id="sign-out.accounts.return.host">Return to {host}</Trans>
            ) : (
              <Trans id="sign-out.accounts.return">Return to the app</Trans>
            )}
          </Button>
        </PublicStep.Actions>
      }
    >
      <AccountList
        label={t({
          id: "sign-out.accounts.list",
          message: "Accounts on this browser",
        })}
      >
        {accounts.map((account) => (
          <AccountRow
            key={account.accountId}
            account={account}
            action={
              <SignOutAccountButton
                username={account.username}
                pending={pending === account.accountId}
                disabled={busy && pending !== account.accountId}
                onClick={() => void signOutAccount(account.accountId)}
              />
            }
          />
        ))}
      </AccountList>
    </PublicStep>
  );
}
