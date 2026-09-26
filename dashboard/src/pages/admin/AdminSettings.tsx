import { Trans } from "@lingui/react/macro";
import { AsyncSection } from "@/components/custom/Section";
import { GeneralPanel } from "@/pages/admin/settings/GeneralPanel";
import { MaintenancePanel } from "@/pages/admin/settings/MaintenancePanel";
import { NetworkPanel } from "@/pages/admin/settings/NetworkPanel";
import { SigningKeysPanel } from "@/pages/admin/settings/SigningKeysPanel";

/**
 * The instance's settings, one section per kind, stacked on one page.
 *
 * Every section is on screen at once, so the page mounts all of them and each
 * pays its own read on arrival: a section that is slow or broken says so in its
 * own place, and the ones around it stay readable.
 *
 * Each panel names itself with its own `Section`, so a panel's heading sits
 * beside the button it names. The `title` given to a boundary here is the same
 * string the panel draws, repeated only so the heading is on screen while the
 * panel cannot draw it yet: a fallback without it would leave the page
 * headingless on the first frame and push everything down once the reads
 * landed.
 *
 * The general panel is not behind a boundary. Its read is `/config`, which the
 * root route already loaded before the first paint, so it never suspends; and
 * it draws three sections rather than one, so a single heading here would name
 * it differently from what it shows.
 */
export function AdminSettings() {
  return (
    <div className="flex flex-col gap-8">
      <GeneralPanel />
      <AsyncSection
        resetKey="maintenance"
        title={<Trans id="settings.maintenance.title">Maintenance mode</Trans>}
      >
        <MaintenancePanel />
      </AsyncSection>
      <AsyncSection
        resetKey="network"
        title={<Trans id="settings.network.title">Client IP</Trans>}
      >
        <NetworkPanel />
      </AsyncSection>
      <AsyncSection
        resetKey="keys"
        title={<Trans id="settings.keys.section">Signing keys</Trans>}
      >
        <SigningKeysPanel />
      </AsyncSection>
    </div>
  );
}
