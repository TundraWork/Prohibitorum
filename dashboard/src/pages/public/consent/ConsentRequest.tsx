import { Avatar, Link } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import type { LucideIcon } from "lucide-react";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { logoutMutationOptions } from "@/api/mutations";
import { clearSessionQueries } from "@/api/queries";
import type { ConsentAccount } from "@/api/raw-paths";
import { Button } from "@/components/custom/Button";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { PublicStep } from "@/components/custom/PublicStep";

export interface ConsentFact {
  key: string;
  icon: LucideIcon;
  name: ReactNode;
  description?: ReactNode;
}

export type ConsentChoice = "approve" | "decline";

/**
 * The consent card the OIDC and SAML pages share: who is asking, who is
 * answering, what the application will get, and the two answers.
 *
 * `onDecide` sends the answer and follows where it leads. While it runs the
 * pressed button is pending and everything else is disabled; once it resolves
 * the page is already leaving, so the button stays pending until it has gone.
 * A failure has been reported by the error toast, and the buttons come back.
 */
export function ConsentRequest({
  app,
  title,
  account,
  factsTitle,
  facts,
  factsNote,
  footnote,
  decline,
  approve,
  onDecide,
  switchAccountTo,
}: {
  app: { displayName: string; logoUri?: string };
  title: ReactNode;
  account: ConsentAccount;
  factsTitle: ReactNode;
  facts: readonly ConsentFact[];
  /** A line under the list, such as what was allowed before. */
  factsNote?: ReactNode;
  footnote: ReactNode;
  decline: ReactNode;
  approve: ReactNode;
  onDecide: (choice: ConsentChoice) => Promise<void>;
  /** Where the sign-in page returns once another account has signed in. */
  switchAccountTo: string;
}) {
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [pending, setPending] = useState<ConsentChoice>();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const switchAccount = useMutation({
    ...logoutMutationOptions(queryClient),
    onSuccess: async () => {
      await clearSessionQueries(queryClient);
      await navigate({ to: "/login", search: { return_to: switchAccountTo } });
    },
  });
  const busy = pending !== undefined || switchAccount.isPending;

  const decide = async (choice: ConsentChoice) => {
    if (busy) return;
    setPending(choice);
    try {
      await onDecide(choice);
    } catch {
      if (mounted.current) setPending(undefined);
    }
  };

  const displayName = account.displayName;
  return (
    <PublicStep
      // The icons only picture who is named beside them, so a screen reader
      // is not read their fallback letters.
      media={
        <div aria-hidden="true" className="flex">
          <EntityAvatar
            className="size-12"
            iconUrl={app.logoUri}
            fallback={app.displayName.slice(0, 1)}
          />
        </div>
      }
      title={title}
      actions={
        <PublicStep.Actions layout="decision">
          <Button
            variant="secondary"
            fullWidth
            isPending={pending === "decline"}
            isDisabled={busy && pending !== "decline"}
            onPress={() => void decide("decline")}
          >
            {decline}
          </Button>
          <Button
            fullWidth
            isPending={pending === "approve"}
            isDisabled={busy && pending !== "approve"}
            onPress={() => void decide("approve")}
          >
            {approve}
          </Button>
        </PublicStep.Actions>
      }
    >
      <div className="flex min-w-0 items-center gap-2 text-sm text-muted">
        <Avatar size="sm" className="shrink-0" aria-hidden="true">
          {account.avatarUrl && <Avatar.Image src={account.avatarUrl} alt="" />}
          <Avatar.Fallback>{displayName.slice(0, 1)}</Avatar.Fallback>
        </Avatar>
        <p className="min-w-0 wrap-anywhere">
          <Trans id="consent.account">Signed in as {displayName}</Trans>
          <span aria-hidden="true"> · </span>
          <Link
            className="text-sm"
            isDisabled={busy}
            onPress={() => switchAccount.mutate()}
          >
            <Trans id="consent.switch_account">Use another account</Trans>
          </Link>
        </p>
      </div>
      <div className="flex flex-col gap-3">
        <p className="text-sm font-medium" id="consent-facts">
          {factsTitle}
        </p>
        <ul aria-labelledby="consent-facts" className="flex flex-col gap-3">
          {facts.map((fact) => (
            <li key={fact.key} className="flex gap-3">
              <fact.icon
                size={16}
                aria-hidden="true"
                className="mt-0.5 shrink-0 text-muted"
              />
              <div className="flex min-w-0 flex-col">
                <span className="text-sm font-medium wrap-anywhere">
                  {fact.name}
                </span>
                {fact.description && (
                  <span className="text-sm text-muted">{fact.description}</span>
                )}
              </div>
            </li>
          ))}
        </ul>
        {factsNote && <p className="text-sm text-muted">{factsNote}</p>}
      </div>
      <div className="flex flex-col gap-1 text-sm text-muted">{footnote}</div>
    </PublicStep>
  );
}
