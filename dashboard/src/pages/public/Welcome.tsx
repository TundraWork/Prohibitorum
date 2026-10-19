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
  clearSessionQueries,
  federationConfirmQueryOptions,
} from "@/api/queries";
import type { FederationConfirm } from "@/api/raw-paths";
import { followRedirect } from "@/app/redirect";
import { Button } from "@/components/custom/Button";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";

type Answer = "confirm" | "decline";

/**
 * The account a first sign-in through a provider has prepared, for the
 * reader to accept before the session is issued, or to turn down.
 *
 * Both answers use the prepared sign-in up, so once one is pressed the other
 * is disabled and the pressed one stays pending until the page has gone.
 */
export function WelcomePage() {
  const [answer, setAnswer] = useState<Answer>();
  const base = federationConfirmQueryOptions();
  const { data: account } = useSuspenseQuery(base);
  const { name: instance } = useInstanceBranding();
  const queryClient = useQueryClient();
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
            pending={answer === "confirm"}
            disabled={answer === "decline"}
            onClick={() => void answerWith("confirm")}
            size="lg"
            className="w-full"
          >
            <Trans id="welcome.continue">Continue</Trans>
          </Button>
          <Button
            pending={answer === "decline"}
            disabled={answer === "confirm"}
            onClick={() => void answerWith("decline")}
            variant="ghost"
            size="lg"
            className="w-full"
          >
            <Trans id="welcome.decline">This isn't me</Trans>
          </Button>
        </PublicStep.Actions>
      }
    >
      <AccountPreview account={account} />
    </PublicStep>
  );
}

/**
 * The prepared account as the reader will see it. The provider's picture is
 * fetched during the sign-in, so it is either here already or the two-letter
 * initials stand in.
 */
function AccountPreview({ account }: { account: FederationConfirm }) {
  return (
    <div className="flex min-w-0 items-center gap-4 rounded-xl bg-muted/60 p-4 ring-1 ring-border/60">
      <EntityAvatar
        shape="round"
        size="xl"
        iconUrl={account.avatarUrl}
        name={account.displayName}
        fallbackClassName="bg-background"
      />
      <div className="flex min-w-0 flex-col">
        <span className="font-medium wrap-anywhere">{account.displayName}</span>
        <span className="font-mono text-foreground/75 text-sm wrap-anywhere">
          @{account.username}
        </span>
        {account.email && (
          <span className="text-foreground/75 text-sm wrap-anywhere">
            {account.email}
          </span>
        )}
      </div>
    </div>
  );
}
