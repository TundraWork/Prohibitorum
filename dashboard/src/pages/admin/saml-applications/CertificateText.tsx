import { Trans, useLingui } from "@lingui/react/macro";
import type { CertificateState } from "@/pages/admin/saml-applications/saml-certificate";

/**
 * A certificate's state in words, the one place its wording lives so the page
 * header, the list and the metadata rows cannot disagree.
 *
 * A date rather than a distance from now: "expires 4 Mar 2027" is what an
 * operator puts in a calendar. An expired certificate takes the danger colour
 * as well as the word. `named` says "Certificate" first, for the header, where
 * nothing around the text says what it is about.
 */
export function CertificateText({
  state,
  named = false,
}: {
  state: CertificateState;
  named?: boolean;
}) {
  const { i18n } = useLingui();
  const day = (notAfter: string) =>
    new Intl.DateTimeFormat(i18n.locale, { dateStyle: "medium" }).format(
      new Date(notAfter),
    );

  switch (state.kind) {
    case "none":
      return (
        <Trans id="admin.saml-apps.certificate.none">
          No signing certificate
        </Trans>
      );
    case "no-expiry":
      return named ? (
        <Trans id="admin.saml-apps.certificate.no-expiry">
          Certificate has no expiry date
        </Trans>
      ) : (
        <Trans id="admin.saml-apps.keys.no-expiry">No expiry</Trans>
      );
    case "unreadable":
      return (
        <Trans id="admin.saml-apps.certificate.unreadable">
          Unreadable date
        </Trans>
      );
    case "expired": {
      const date = day(state.notAfter);
      return (
        <span className="text-destructive-foreground">
          {named ? (
            <Trans id="admin.saml-apps.certificate.expired">
              Certificate expired
            </Trans>
          ) : (
            <Trans id="admin.saml-apps.keys.expired">Expired {date}</Trans>
          )}
        </span>
      );
    }
    case "valid": {
      const date = day(state.notAfter);
      return named ? (
        <Trans id="admin.saml-apps.certificate.expires">
          Certificate expires {date}
        </Trans>
      ) : (
        <Trans
          id="admin.saml-apps.keys.expires"
          comment="A date the administrator plans around, so it is a date and not a relative time"
        >
          Expires {date}
        </Trans>
      );
    }
  }
}
