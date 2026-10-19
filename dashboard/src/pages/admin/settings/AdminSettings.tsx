import { Trans } from "@lingui/react/macro";
import { AppDetailLayout } from "@/components/custom/AppDetailLayout";
import { PageHeader } from "@/components/custom/PageHeader";
import { AsyncSection } from "@/components/custom/Section";
import { GeneralPanel } from "@/pages/admin/settings/GeneralPanel";
import { MaintenancePanel } from "@/pages/admin/settings/MaintenancePanel";
import { NetworkPanel } from "@/pages/admin/settings/NetworkPanel";
import { SignInPagePanel } from "@/pages/admin/settings/SignInPagePanel";
import { SigningKeysPanel } from "@/pages/admin/settings/SigningKeysPanel";

/**
 * The instance's settings: the page heading, then one section per kind in a
 * column with an outline beside it.
 *
 * Every section is on screen at once, so the page mounts all of them and each
 * pays its own read on arrival: a section that is slow or broken says so in its
 * own place, and the ones around it stay readable. Each panel draws its own
 * `Section` (and so its own save), which is also what the outline lists.
 *
 * The `title` given to a boundary here is the same string the panel draws,
 * repeated only so the heading is on screen while the panel cannot draw it yet:
 * a fallback without it would leave the page headingless on the first frame
 * and push everything down once the reads landed.
 *
 * The general and maintenance panels are not behind a boundary. Their read is
 * `/config`, which the root route already loaded before the first paint, so
 * they never suspend. The sign-in page follows the general panel, since it is
 * also about how the instance looks, behind its own boundary for the
 * administrator-only read of its settings.
 */
export function AdminSettings() {
  return (
    <AppDetailLayout
      header={
        <PageHeader
          title={<Trans id="console.admin.settings">Settings</Trans>}
          description={
            <Trans id="settings.page.description">
              How this instance is named, signed in to and kept running.
            </Trans>
          }
        />
      }
    >
      <GeneralPanel />
      <AsyncSection
        resetKey="sign-in-page"
        title={<Trans id="settings.sign-in.title">Sign-in page</Trans>}
      >
        <SignInPagePanel />
      </AsyncSection>
      <MaintenancePanel />
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
    </AppDetailLayout>
  );
}
