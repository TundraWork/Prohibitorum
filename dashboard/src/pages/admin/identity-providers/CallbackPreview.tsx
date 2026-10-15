import { Description, Fieldset } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { CopyValue } from "@/components/custom/CopyValue";

/**
 * The redirect addresses an OIDC provider will be asked to accept for an
 * identifier, previewed before anything is saved: the provider's own
 * registration page usually sits beside this one and asks for them, and a
 * wrong one is only found when a sign-in fails.
 *
 * Only OIDC has addresses to register. Steam is sent its return address on
 * every request, and VRChat proves ownership through a link on the profile, so
 * neither has anything to enter upstream.
 *
 * The origin is the one the console is open on, the same choice the
 * forward-auth snippet makes: it is an address this browser has reached the
 * server on. The server builds the same paths on its first configured public
 * origin, so an operator browsing a secondary origin should check the
 * server's configuration before registering what is shown here.
 *
 * The caller passes an identifier the server accepts, already trimmed.
 */
export function CallbackPreview({ slug }: { slug: string }) {
  const origin = window.location.origin;

  return (
    <Fieldset>
      <Fieldset.Legend className="text-sm font-medium text-foreground">
        <Trans id="admin.federation.callback.title">Callback addresses</Trans>
      </Fieldset.Legend>
      <Description>
        <Trans id="admin.federation.callback.hint">
          Register these addresses with the provider.
        </Trans>
      </Description>
      <Fieldset.Group>
        <CopyValue
          label={<Trans id="admin.federation.callback.login">Sign-in</Trans>}
          value={`${origin}/api/prohibitorum/auth/federation/${slug}/callback`}
        />
        <CopyValue
          label={
            <Trans id="admin.federation.callback.link">Link an account</Trans>
          }
          value={`${origin}/api/prohibitorum/me/identities/link/${slug}/callback`}
        />
        <CopyValue
          label={
            <Trans id="admin.federation.callback.test">Connection test</Trans>
          }
          value={`${origin}/api/prohibitorum/auth/federation/${slug}/test/callback`}
        />
      </Fieldset.Group>
    </Fieldset>
  );
}
