import { Tabs } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useNavigate } from "@tanstack/react-router";

import type { SecurityTab } from "@/pages/console/tabs";
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
    <>
      <Tabs
        selectedKey={tab}
        onSelectionChange={(key) => {
          // replace, so browsing tabs never buries the page the user came from.
          void navigate({
            to: "/security",
            search: { tab: key as SecurityTab },
            replace: true,
          });
        }}
      >
        <Tabs.ListContainer className="ml-2 w-fit max-w-full">
          <Tabs.List
            aria-label={t({ id: "security.tabs", message: "Security" })}
          >
            <Tabs.Tab className="whitespace-nowrap" id="passkeys">
              <Trans id="security.tab.passkeys">Passkeys</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
            <Tabs.Tab className="whitespace-nowrap" id="password">
              <Trans id="security.tab.password">
                Password and authenticator
              </Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
            <Tabs.Tab className="whitespace-nowrap" id="sessions">
              <Trans id="security.tab.sessions">Active sessions</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
            <Tabs.Tab className="whitespace-nowrap" id="identities">
              <Trans id="security.tab.identities">Connected identities</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
            <Tabs.Tab className="whitespace-nowrap" id="tokens">
              <Trans id="security.tab.tokens">Access tokens</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
          </Tabs.List>
        </Tabs.ListContainer>
        <Tabs.Panel id="passkeys" className="pt-4">
          <PasskeysPanel />
        </Tabs.Panel>
        <Tabs.Panel id="password" className="pt-4">
          <PasswordTotpPanel />
        </Tabs.Panel>
        <Tabs.Panel id="sessions" className="pt-4">
          <SessionsPanel />
        </Tabs.Panel>
        <Tabs.Panel id="identities" className="pt-4">
          <IdentitiesPanel />
        </Tabs.Panel>
        <Tabs.Panel id="tokens" className="pt-4">
          <TokensPanel />
        </Tabs.Panel>
      </Tabs>
    </>
  );
}
