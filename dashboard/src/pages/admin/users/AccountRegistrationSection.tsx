import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation } from "@tanstack/react-query";
import { LinkIcon } from "lucide-react";
import { useState } from "react";
import { reissueEnrollmentMutationOptions } from "@/api/mutations";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { SecretRevealDialog } from "@/components/custom/SecretReveal";
import { Section } from "@/components/custom/Section";
import { SettingRow } from "@/components/custom/SettingRow";
import { invitationLinkCopy } from "@/components/custom/secret-reveal-copy";

/**
 * Issues a fresh registration link for the account.
 *
 * The link is a credential: anyone holding it can set this account's sign-in
 * method, and the server does not show it again, so it is revealed in a dialog
 * that cannot be dismissed while the section holds it. Issuing asks for a fresh
 * identity check and stops any earlier link. Once the reveal is dismissed the
 * section keeps the expiry, which is the one fact about the link that can
 * still be read.
 */
export function AccountRegistrationSection({
  accountId,
}: {
  accountId: number;
}) {
  const { i18n } = useLingui();
  const reissue = useMutation(
    withInlineReport(reissueEnrollmentMutationOptions(), true),
  );
  const failure = useInlineFailure();
  const [url, setUrl] = useState<string | null>(null);
  const [expiresAt, setExpiresAt] = useState<string | null>(null);

  const expiry =
    expiresAt === null
      ? ""
      : new Intl.DateTimeFormat(i18n.locale, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(expiresAt));

  return (
    <Section
      id="registration"
      icon={<LinkIcon />}
      title={<Trans id="admin.user.enrollment.title">Registration link</Trans>}
    >
      <SettingRow
        title={
          <Trans id="admin.user.enrollment.row">Set up how they sign in</Trans>
        }
        description={
          <Trans id="admin.user.enrollment.note">
            Issues a new link this account can use to set up how they sign in.
            The link is shown once, and any earlier link stops working.
          </Trans>
        }
      >
        <Button
          pending={reissue.isPending}
          onClick={() => {
            failure.clear();
            reissue.mutate(accountId, {
              onSuccess: (result) => {
                setUrl(result.url);
                setExpiresAt(result.expiresAt);
              },
              onError: failure.report,
            });
          }}
          variant="outline"
        >
          <Trans id="admin.user.enrollment.action">
            Issue a registration link
          </Trans>
        </Button>
      </SettingRow>
      <InlineFailure error={failure.error} />
      {expiresAt !== null && (
        <p className="text-muted-foreground text-xs">
          <Trans id="admin.user.enrollment.expires">
            The link expires on {expiry}.
          </Trans>
        </p>
      )}
      {url !== null && (
        // Open exactly while the link is held here, so it cannot outlive it and
        // only the reveal's own continue control can close it.
        <SecretRevealDialog
          text={url}
          filename="prohibitorum-registration-link.txt"
          copy={invitationLinkCopy}
          onContinue={async () => setUrl(null)}
        />
      )}
    </Section>
  );
}
