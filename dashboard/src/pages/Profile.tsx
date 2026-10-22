import { Trans } from "@lingui/react/macro";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { AccountPanel } from "@/pages/profile/AccountPanel";
import { AppearancePanel } from "@/pages/profile/AppearancePanel";
import { AvatarPanel } from "@/pages/profile/AvatarPanel";

/**
 * The account's own page: the picture it shows, then its name and
 * identifiers, then how the pages look, as stacked sections rather than tabs.
 * Each is short, and a reader who came to change one often looks at another.
 *
 * Each panel draws its own `Section`, as on the Security page; this file owns
 * only their order.
 */
export function Profile() {
  return (
    <PageBody>
      <PageHeader
        title={<Trans id="profile.page.title">Profile</Trans>}
        description={
          <Trans id="profile.page.description">
            The picture and name shown wherever you sign in.
          </Trans>
        }
      />
      <AvatarPanel />
      <AccountPanel />
      <AppearancePanel />
    </PageBody>
  );
}
