import { Trans } from "@lingui/react/macro";
import { type ReactNode, useEffect, useRef, useState } from "react";
import type { ConsentAccount } from "@/api/raw-paths";
import { AppStrip } from "@/components/custom/AppStrip";
import { Button } from "@/components/custom/Button";
import { PublicStep } from "@/components/custom/PublicStep";
import { type ScopeRow, ScopeRows } from "@/components/custom/ScopeRows";
import { Avatar, AvatarFallback, AvatarImage } from "@/components/ui/avatar";
import { useSwitchAccount } from "@/pages/public/consent/useSwitchAccount";

export type ConsentChoice = "approve" | "decline";

const listLabelId = "consent-rows";

/** A link inside the consent footnote: underlined, since it sits in running text. */
export const consentLinkClass =
  "rounded-sm text-foreground underline underline-offset-2 outline-none hover:text-primary focus-visible:ring-2 focus-visible:ring-ring";

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
  rowsLabel,
  rows,
  after,
  footnote,
  decline,
  approve,
  onDecide,
  switchAccountTo,
}: {
  app: { displayName: string; logoUri?: string };
  title: ReactNode;
  account: ConsentAccount;
  /** Introduces the rows; the list is named after it. */
  rowsLabel: ReactNode;
  rows: readonly ScopeRow[];
  /** What follows the rows, such as the scopes allowed before. */
  after?: ReactNode;
  footnote: ReactNode;
  decline: ReactNode;
  approve: ReactNode;
  onDecide: (choice: ConsentChoice) => Promise<void>;
  /** Where the sign-in page returns once another account has signed in. */
  switchAccountTo: string;
}) {
  const [pending, setPending] = useState<ConsentChoice>();
  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);
  const switchAccount = useSwitchAccount(switchAccountTo);
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
      wide
      strip={
        <AppStrip
          lead={<Trans id="consent.strip.lead">Asking for access</Trans>}
          name={app.displayName}
          iconUrl={app.logoUri}
        />
      }
      title={title}
      footer={
        <div className="grid w-full grid-cols-2 gap-2">
          <Button
            pending={pending === "decline"}
            disabled={busy && pending !== "decline"}
            onClick={() => void decide("decline")}
            variant="outline"
            size="lg"
          >
            {decline}
          </Button>
          <Button
            pending={pending === "approve"}
            disabled={busy && pending !== "approve"}
            onClick={() => void decide("approve")}
            size="lg"
          >
            {approve}
          </Button>
        </div>
      }
    >
      <div className="flex min-w-0 items-center gap-2.5 rounded-xl bg-muted px-3 py-2 text-sm">
        <Avatar className="size-6 shrink-0" aria-hidden="true">
          {account.avatarUrl && <AvatarImage src={account.avatarUrl} alt="" />}
          <AvatarFallback>{displayName.slice(0, 1)}</AvatarFallback>
        </Avatar>
        <p className="min-w-0 wrap-anywhere">
          <Trans id="consent.account">
            Signed in as <b className="font-medium">{displayName}</b>
          </Trans>
        </p>
        <Button
          variant="ghost"
          size="sm"
          className="ms-auto"
          pending={switchAccount.isPending}
          disabled={busy && !switchAccount.isPending}
          onClick={() => switchAccount.mutate()}
        >
          <Trans id="consent.switch_account">Switch account</Trans>
        </Button>
      </div>
      <div className="flex flex-col gap-2">
        <p
          className="flex items-center gap-2 font-medium text-sm"
          id={listLabelId}
        >
          {rowsLabel}
        </p>
        <ScopeRows rows={rows} labelledBy={listLabelId} />
        {after}
      </div>
      <p className="text-muted-foreground text-xs">{footnote}</p>
    </PublicStep>
  );
}
