import { Trans, useLingui } from "@lingui/react/macro";
import { useNavigate } from "@tanstack/react-router";
import { ConsoleTabs } from "@/components/custom/ConsoleTabs";

import { IdentitiesPanel } from "@/pages/security/IdentitiesPanel";
import { PasskeysPanel } from "@/pages/security/PasskeysPanel";
import { PasswordTotpPanel } from "@/pages/security/PasswordTotpPanel";
import { SessionsPanel } from "@/pages/security/SessionsPanel";
import { TokensPanel } from "@/pages/security/TokensPanel";
import { Route } from "@/routes/_protected.security";

/**
 * Everything about how this account gets in. Five tabs rather than one long
 * column, with the selected tab in the URL so a reload or a shared link lands on
 * the same one.
 *
 * Each panel is mounted only while its tab is selected, which is also what keeps
 * the queries behind it from running early: a panel that is not on screen has no
 * reason to ask the server for anything.
 */
export function Security() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const { tab } = Route.useSearch();

  return (
    <ConsoleTabs
      label={t({ id: "security.tabs", message: "Security" })}
      selected={tab}
      onSelectionChange={(next) =>
        void navigate({ to: "/security", search: { tab: next }, replace: true })
      }
      tabs={[
        {
          id: "passkeys",
          title: <Trans id="security.tab.passkeys">Passkeys</Trans>,
          panel: () => <PasskeysPanel />,
        },
        {
          id: "password",
          title: (
            <Trans id="security.tab.password">Password and authenticator</Trans>
          ),
          panel: () => <PasswordTotpPanel />,
        },
        {
          id: "sessions",
          title: <Trans id="security.tab.sessions">Active sessions</Trans>,
          panel: () => <SessionsPanel />,
        },
        {
          id: "identities",
          title: (
            <Trans id="security.tab.identities">Connected identities</Trans>
          ),
          panel: () => <IdentitiesPanel />,
        },
        {
          id: "tokens",
          title: <Trans id="security.tab.tokens">Access tokens</Trans>,
          panel: () => <TokensPanel />,
        },
      ]}
    />
  );
}
