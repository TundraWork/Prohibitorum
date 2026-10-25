import { Trans } from "@lingui/react/macro";
import { AppDetailLayout } from "@/components/custom/AppDetailLayout";
import { PageHeader } from "@/components/custom/PageHeader";
import { AsyncSection } from "@/components/custom/Section";
import { GeneralPanel } from "@/pages/admin/settings/GeneralPanel";
import { MailPanel } from "@/pages/admin/settings/MailPanel";
import { MaintenancePanel } from "@/pages/admin/settings/MaintenancePanel";
import {
  clientIpSection,
  NetworkPanel,
} from "@/pages/admin/settings/NetworkPanel";
import { SignInPageSummary } from "@/pages/admin/settings/SignInPageSummary";
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
 * The general and maintenance panels and the sign-in page summary are not
 * behind a boundary. Their read is `/config`, which the root route already
 * loaded before the first paint, so they never suspend. The signing keys are
 * not either: their list is not a suspense read, and the section draws its own
 * loading rows and failure. The sign-in page follows the general panel, since
 * it is also about how the instance looks; it only summarises the page, which
 * has its own route for editing.
 */
export function AdminSettings() {
  return (
    <AppDetailLayout
      header={
        <PageHeader
          title={<Trans id="console.admin.settings">Settings</Trans>}
          description={
            <Trans id="settings.page.description">
              Manage the instance name and icon, sign-in page, maintenance mode,
              mail, client IP and signing keys.
            </Trans>
          }
        />
      }
    >
      <GeneralPanel />
      <SignInPageSummary />
      <MaintenancePanel />
      <MailPanel />
      <AsyncSection resetKey="network" {...clientIpSection}>
        <NetworkPanel />
      </AsyncSection>
      <SigningKeysPanel />
    </AppDetailLayout>
  );
}
