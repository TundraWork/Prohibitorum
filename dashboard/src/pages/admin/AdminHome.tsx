import { Trans } from "@lingui/react/macro";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";

/**
 * The management area's landing page. Its sections are named in the sidebar, so
 * this says what the area is for rather than repeating them.
 *
 * Which of those sections a reader actually has is not stated here: a delegated
 * application manager sees only the application sections and would be told about
 * pages they cannot open. The sidebar is the list, and it is already the right
 * one for whoever is reading.
 */
export function AdminHome() {
  return (
    <PageBody>
      <PageHeader
        title={<Trans id="admin.home.title">Administration</Trans>}
        description={
          <Trans id="admin.home.summary">
            Accounts, user groups, invitations, identity providers, downstream
            applications, logs and settings are managed from the sections in the
            sidebar.
          </Trans>
        }
      />
    </PageBody>
  );
}
