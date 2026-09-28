import { Avatar, Skeleton, Surface } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useNavigate, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import {
  federationConfirmMutationOptions,
  federationDeclineMutationOptions,
} from "@/api/mutations";
import {
  awaitingAvatar,
  clearSessionQueries,
  federationConfirmQueryOptions,
} from "@/api/queries";
import type { FederationConfirm } from "@/api/raw-paths";
import { followRedirect } from "@/app/redirect";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";

type Answer = "confirm" | "decline";

/**
 * The account a first sign-in through a provider has prepared, for the
 * reader to accept before the session is issued, or to turn down.
 *
 * Both answers use the prepared sign-in up, so once one is pressed the other
 * is disabled, the pressed one stays pending until the page has gone, and
 * the account is no longer read while the provider's picture is awaited.
 */
export function WelcomePage() {
  const [answer, setAnswer] = useState<Answer>();
  const base = federationConfirmQueryOptions();
  const { data: account, dataUpdatedAt } = useSuspenseQuery({
    ...base,
    refetchInterval: answer === undefined ? base.refetchInterval : false,
  });
  const { name: instance } = useInstanceBranding();
  const queryClient = useQueryClient();
  // `dataUpdatedAt` moves on every read, even one that returns the same
  // account, so reading it re-renders the page when the wait runs out.
  const state = queryClient.getQueryState(base.queryKey);
  const waiting =
    dataUpdatedAt > 0 && state !== undefined && awaitingAvatar(state);
  const router = useRouter();
  const navigate = useNavigate();
  const confirm = useMutation(federationConfirmMutationOptions());
  const decline = useMutation(federationDeclineMutationOptions());

  const answerWith = async (choice: Answer) => {
    if (answer !== undefined) return;
    setAnswer(choice);
    await queryClient.cancelQueries({ queryKey: base.queryKey });
    try {
      if (choice === "confirm") {
        const { redirect, offerLocalSignin } = await confirm.mutateAsync();
        await clearSessionQueries(queryClient);
        if (offerLocalSignin) {
          await navigate({ to: "/setup-signin", search: { redirect } });
        } else {
          await followRedirect(router, redirect);
        }
      } else {
        await decline.mutateAsync();
        await navigate({ to: "/login" });
      }
    } catch {
      // The toast has reported it; the buttons come back.
      setAnswer(undefined);
    }
  };

  const idp = account.idpDisplayName;
  return (
    <PublicStep
      title={<Trans id="welcome.title">Is this your account?</Trans>}
      description={
        <Trans id="welcome.description">
          You're signing in through {idp} for the first time, and {instance} has
          prepared this account for you.
        </Trans>
      }
      actions={
        <PublicStep.Actions>
          <Button
            fullWidth
            isPending={answer === "confirm"}
            isDisabled={answer === "decline"}
            onPress={() => void answerWith("confirm")}
          >
            <Trans id="welcome.continue">Continue</Trans>
          </Button>
          <Button
            variant="tertiary"
            fullWidth
            isPending={answer === "decline"}
            isDisabled={answer === "confirm"}
            onPress={() => void answerWith("decline")}
          >
            <Trans id="welcome.decline">This isn't me</Trans>
          </Button>
        </PublicStep.Actions>
      }
    >
      <AccountPreview account={account} waiting={waiting} />
    </PublicStep>
  );
}

/**
 * The prepared account as the reader will see it. While the provider's
 * picture is still on its way its place holds a placeholder of the same
 * shape; once the page stops waiting, the initial stands in.
 */
function AccountPreview({
  account,
  waiting,
}: {
  account: FederationConfirm;
  waiting: boolean;
}) {
  const picture = account.avatarPending ? undefined : account.avatarUrl;
  return (
    <Surface
      variant="secondary"
      className="flex min-w-0 items-center gap-3 rounded-lg p-3"
    >
      {waiting ? (
        <Skeleton className="size-12 shrink-0 rounded-[calc(var(--radius)*3)]" />
      ) : (
        <Avatar size="lg" aria-hidden="true">
          {picture && <Avatar.Image src={picture} alt="" />}
          <Avatar.Fallback>{account.displayName.slice(0, 1)}</Avatar.Fallback>
        </Avatar>
      )}
      <div className="flex min-w-0 flex-col">
        <span className="font-medium wrap-anywhere">{account.displayName}</span>
        <span className="flex flex-wrap gap-x-1 text-sm text-muted">
          <span className="wrap-anywhere">@{account.username}</span>
          {account.email && (
            <>
              <span aria-hidden="true">·</span>
              <span className="wrap-anywhere">{account.email}</span>
            </>
          )}
        </span>
      </div>
    </Surface>
  );
}
