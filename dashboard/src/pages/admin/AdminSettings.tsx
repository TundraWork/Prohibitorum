import { Tabs } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useNavigate } from "@tanstack/react-router";
import { GeneralPanel } from "@/pages/admin/settings/GeneralPanel";
import { MaintenancePanel } from "@/pages/admin/settings/MaintenancePanel";
import { NetworkPanel } from "@/pages/admin/settings/NetworkPanel";
import { SigningKeysPanel } from "@/pages/admin/settings/SigningKeysPanel";
import type { SettingsTab } from "@/pages/console/tabs";
import { Route } from "@/routes/_protected.admin.settings";

/**
 * The instance's settings, one tab per kind, with the selected tab in the URL.
 *
 * Each panel is mounted only while its tab is selected, so the network tab's
 * read and the signing-key list are requested when someone opens them rather
 * than whenever the page does.
 */
export function AdminSettings() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const { tab } = Route.useSearch();

  return (
    <Tabs
      selectedKey={tab}
      onSelectionChange={(key) => {
        // replace, so browsing tabs never buries the page the user came from.
        void navigate({
          to: "/admin/settings",
          search: { tab: key as SettingsTab },
          replace: true,
        });
      }}
    >
      <Tabs.ListContainer className="ml-2 w-fit max-w-full">
        <Tabs.List aria-label={t({ id: "settings.tabs", message: "Settings" })}>
          <Tabs.Tab className="whitespace-nowrap" id="general">
            <Trans id="settings.tab.general">General</Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
          <Tabs.Tab className="whitespace-nowrap" id="maintenance">
            <Trans id="settings.tab.maintenance">Maintenance</Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
          <Tabs.Tab className="whitespace-nowrap" id="network">
            <Trans id="settings.tab.network">Network</Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
          <Tabs.Tab className="whitespace-nowrap" id="keys">
            <Trans id="settings.tab.keys">Signing keys</Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
        </Tabs.List>
      </Tabs.ListContainer>
      <Tabs.Panel id="general" className="pt-4">
        {tab === "general" && <GeneralPanel />}
      </Tabs.Panel>
      <Tabs.Panel id="maintenance" className="pt-4">
        {tab === "maintenance" && <MaintenancePanel />}
      </Tabs.Panel>
      <Tabs.Panel id="network" className="pt-4">
        {tab === "network" && <NetworkPanel />}
      </Tabs.Panel>
      <Tabs.Panel id="keys" className="pt-4">
        {tab === "keys" && <SigningKeysPanel />}
      </Tabs.Panel>
    </Tabs>
  );
}
