import { Trans, useLingui } from "@lingui/react/macro";
import { SwitchField } from "@/components/custom/FormFields";
import { UriList } from "@/components/custom/UriList";

/**
 * The fields of an OIDC client's sign-in rules that the create page and the
 * application's own page draw identically. Each renders inside the form field
 * of the same name, as `OidcScopesField` does.
 */

/** The addresses a client may send people back to; never fewer than one. */
export function RedirectUriList() {
  const { t } = useLingui();
  return (
    <UriList
      label={t({
        id: "admin.oidc-apps.field.redirects",
        message: "Redirect URIs",
      })}
      addLabel={<Trans id="admin.oidc-apps.uri.add">Add address</Trans>}
      placeholder="https://app.example.com/callback"
      minRows={1}
      minRowsReason={
        <Trans id="admin.oidc-apps.field.redirect.required">
          At least one redirect URI is required.
        </Trans>
      }
    />
  );
}

/**
 * Whether the client must prove the request it started. A public client cannot
 * turn it off, so its switch is locked on with the reason beside it: a disabled
 * control gives none on its own.
 */
export function PkceSwitch({ isPublic }: { isPublic: boolean }) {
  const label = <Trans id="admin.oidc-apps.field.pkce">Require PKCE</Trans>;
  return isPublic ? (
    <SwitchField
      label={label}
      description={
        <Trans id="admin.oidc-apps.field.pkce.public">
          Always required for public clients.
        </Trans>
      }
      isDisabled
    />
  ) : (
    <SwitchField label={label} />
  );
}

/** Whether the account approves the scopes before being signed in. */
export function ConsentSwitch() {
  return (
    <SwitchField
      label={<Trans id="admin.oidc-apps.field.consent">Require consent</Trans>}
      description={
        <Trans id="admin.oidc-apps.field.consent.hint">
          Ask users to approve the requested scopes before signing in.
        </Trans>
      }
    />
  );
}
