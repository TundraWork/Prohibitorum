import { Fieldset } from "@heroui/react";
import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { useLingui } from "@lingui/react/macro";
import type { ProviderProtocol } from "@/api/raw-admin-paths";
import { CopyValue } from "@/components/custom/CopyValue";

const loginLabel = msg({
  id: "admin.federation.callback.login",
  message: "Sign-in",
});

const linkLabel = msg({
  id: "admin.federation.callback.link",
  message: "Link an account",
});

const testLabel = msg({
  id: "admin.federation.callback.test",
  message: "Test",
});

/** One address the preview offers: what it is for, and where it sends. */
interface CallbackRow {
  key: string;
  label: MessageDescriptor;
  url: string;
}

/**
 * The callback addresses an identifier produces, in the order the provider is
 * asked for them: sign-in, then linking an existing account, then — for OIDC
 * only — the connection test, which the server offers to no other protocol.
 * VRChat is absent altogether: its flow proves ownership through a link on
 * the profile, so there is no redirect address to register anywhere.
 *
 * The origin is the one the console is open on, the same choice the
 * forward-auth snippet makes: it is an address this browser has reached the
 * server on. The server builds the same paths on its first configured
 * public origin, so an operator browsing a secondary origin should check the
 * server's configuration before registering what is shown here.
 */
function callbackRows(slug: string, protocol: ProviderProtocol): CallbackRow[] {
  const origin = window.location.origin;
  const rows: CallbackRow[] = [
    {
      key: "login",
      label: loginLabel,
      url: `${origin}/api/prohibitorum/auth/federation/${slug}/callback`,
    },
    {
      key: "link",
      label: linkLabel,
      url: `${origin}/api/prohibitorum/me/identities/link/${slug}/callback`,
    },
  ];
  if (protocol !== "oidc") return rows;
  rows.push({
    key: "test",
    label: testLabel,
    url: `${origin}/api/prohibitorum/auth/federation/${slug}/test/callback`,
  });
  return rows;
}

/**
 * What the identifier typed above means at the provider: the exact addresses
 * its sign-ins will be sent back to. They are previewed before anything is
 * saved, because the provider's own registration page usually sits beside
 * this one and asks for them, and a wrong one is only found when a sign-in
 * fails.
 *
 * The caller passes an identifier the server accepts, already trimmed.
 */
export function CallbackPreview({
  slug,
  protocol,
}: {
  slug: string;
  protocol: ProviderProtocol;
}) {
  const { t } = useLingui();
  if (protocol === "vrchat") return null;

  return (
    <Fieldset>
      <Fieldset.Legend className="mb-3 text-sm font-medium text-foreground">
        {t({
          id: "admin.federation.callback.title",
          message: "Callback addresses",
        })}
      </Fieldset.Legend>
      <div className="flex flex-col gap-4">
        {callbackRows(slug, protocol).map(({ key, label, url }) => (
          <CopyValue key={key} label={t(label)} value={url} />
        ))}
      </div>
    </Fieldset>
  );
}
