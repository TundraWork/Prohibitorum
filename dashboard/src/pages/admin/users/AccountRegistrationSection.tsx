import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation } from "@tanstack/react-query";
import { UserRoundPlusIcon } from "lucide-react";
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
import { registrationLinkCopyWithExpiry } from "@/components/custom/secret-reveal-copy";

/**
 * Issues a fresh registration link for the account.
 *
 * The link is a credential: anyone holding it can set this account's sign-in
 * method, and the server does not show it again, so it is revealed in a dialog
 * that cannot be dismissed while the section holds it. Issuing asks for a fresh
 * identity check and stops any earlier link. The expiry is told in the reveal,
 * while the link is still on screen.
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
      icon={<UserRoundPlusIcon />}
      title={<Trans id="admin.user.enrollment.title">Registration link</Trans>}
      description={
        <Trans id="admin.user.enrollment.description">
          Set up how this account signs in with a registration link.
        </Trans>
      }
    >
      <SettingRow
        title={
          <Trans id="admin.user.enrollment.row">Set up how they sign in</Trans>
        }
        description={
          <Trans id="admin.user.enrollment.note">
            Issues a new registration link. The link is shown once, and any
            earlier link stops working.
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
      {url !== null && (
        // Open exactly while the link is held here, so it cannot outlive it and
        // only the reveal's own continue control can close it.
        <SecretRevealDialog
          text={url}
          filename="prohibitorum-registration-link.txt"
          copy={registrationLinkCopyWithExpiry(expiry)}
          onContinue={async () => setUrl(null)}
        />
      )}
    </Section>
  );
}
