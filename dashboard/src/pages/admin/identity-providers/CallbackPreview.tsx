import { Trans, useLingui } from "@lingui/react/macro";
import type { ReactNode } from "react";
import { CopyableValue } from "@/components/custom/CopyableValue";
import { Fieldset, FieldsetLegend } from "@/components/ui/fieldset";

/**
 * The redirect addresses an OIDC provider will be asked to accept for an
 * identifier: the provider's own registration page usually sits beside this one
 * and asks for them, and a wrong one is only found when a sign-in fails.
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
 * The caller passes an identifier the server accepts, already trimmed. A caller
 * that sits under a heading of its own passes `titled={false}`: the group keeps
 * its name for a screen reader and the heading is not said twice.
 */
export function CallbackPreview({
  slug,
  titled = true,
}: {
  slug: string;
  titled?: boolean;
}) {
  const { t } = useLingui();
  const origin = window.location.origin;

  return (
    <Fieldset className="flex flex-col gap-2">
      <FieldsetLegend className={titled ? "font-medium text-sm" : "sr-only"}>
        <Trans id="admin.federation.callback.title">Callback addresses</Trans>
      </FieldsetLegend>
      <p className="text-muted-foreground text-sm">
        <Trans id="admin.federation.callback.hint">
          Register these with the provider.
        </Trans>
      </p>
      <div className="flex flex-col gap-3 rounded-xl bg-muted/60 p-3 ring-1 ring-border/60">
        <Address
          label={<Trans id="admin.federation.callback.login">Sign-in</Trans>}
          copyLabel={t({
            id: "admin.federation.callback.login.copy",
            message: "Copy the sign-in address",
          })}
          value={`${origin}/api/prohibitorum/auth/federation/${slug}/callback`}
        />
        <Address
          label={
            <Trans id="admin.federation.callback.link">Link an account</Trans>
          }
          copyLabel={t({
            id: "admin.federation.callback.link.copy",
            message: "Copy the link-an-account address",
          })}
          value={`${origin}/api/prohibitorum/me/identities/link/${slug}/callback`}
        />
        <Address
          label={
            <Trans id="admin.federation.callback.test">Connection test</Trans>
          }
          copyLabel={t({
            id: "admin.federation.callback.test.copy",
            message: "Copy the connection test address",
          })}
          value={`${origin}/api/prohibitorum/auth/federation/${slug}/test/callback`}
        />
      </div>
    </Fieldset>
  );
}

/** One address: its purpose over the value, which wraps after its slashes. */
function Address({
  label,
  copyLabel,
  value,
}: {
  label: ReactNode;
  copyLabel: string;
  value: string;
}) {
  return (
    <div className="flex min-w-0 flex-col gap-0.5">
      <span className="text-muted-foreground text-sm">{label}</span>
      <CopyableValue
        value={value}
        label={copyLabel}
        wrap
        className="text-[13px] [overflow-wrap:anywhere]"
      />
    </div>
  );
}
