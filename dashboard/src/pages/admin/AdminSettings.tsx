import { Trans, useLingui } from "@lingui/react/macro";
import { useNavigate } from "@tanstack/react-router";
import { ConsoleTabs } from "@/components/custom/ConsoleTabs";
import { GeneralPanel } from "@/pages/admin/settings/GeneralPanel";
import { MaintenancePanel } from "@/pages/admin/settings/MaintenancePanel";
import { NetworkPanel } from "@/pages/admin/settings/NetworkPanel";
import { SigningKeysPanel } from "@/pages/admin/settings/SigningKeysPanel";
import { Route } from "@/routes/_protected.admin.settings";

/**
 * The instance's settings, one tab per kind, with the selected tab in the URL.
 * Each panel reads its own data when it is opened: the network tab's policy
 * and the key list are asked for only then, and while they load only that
 * panel waits.
 */
export function AdminSettings() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const { tab } = Route.useSearch();

  return (
    <ConsoleTabs
      label={t({ id: "settings.tabs", message: "Settings" })}
      selected={tab}
      onSelectionChange={(next) =>
        void navigate({
          to: "/admin/settings",
          search: { tab: next },
          replace: true,
        })
      }
      tabs={[
        {
          id: "general",
          title: <Trans id="settings.tab.general">General</Trans>,
          panel: () => <GeneralPanel />,
        },
        {
          id: "maintenance",
          title: <Trans id="settings.tab.maintenance">Maintenance</Trans>,
          panel: () => <MaintenancePanel />,
        },
        {
          id: "network",
          title: <Trans id="settings.tab.network">Network</Trans>,
          panel: () => <NetworkPanel />,
        },
        {
          id: "keys",
          title: <Trans id="settings.tab.keys">Signing keys</Trans>,
          panel: () => <SigningKeysPanel />,
        },
      ]}
    />
  );
}
