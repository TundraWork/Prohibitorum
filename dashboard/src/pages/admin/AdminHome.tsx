import { Alert } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { ConsoleCard } from "@/components/custom/ConsoleCard";

/**
 * The management area's landing page. The three sections it has so far are
 * named in the sidebar, so this says what the area is for rather than repeating
 * them — and names the ones the parent card still owes, so a reader does not
 * go looking for federation settings that are not built yet.
 */
export function AdminHome() {
  return (
    <>
      <ConsoleCard title={<Trans id="admin.home.title">Administration</Trans>}>
        <p className="text-sm text-muted">
          <Trans id="admin.home.summary">
            Users, user groups and invitations are managed from the sections in
            the sidebar. Each one lists what exists and opens a page of its own
            for creating or changing a single entry.
          </Trans>
        </p>
      </ConsoleCard>
      <Alert status="accent">
        <Alert.Indicator />
        <Alert.Content>
          <Alert.Title>
            <Trans id="admin.home.availability">
              Federation, downstream applications, logs and settings are not
              available yet. This area will grow to cover them.
            </Trans>
          </Alert.Title>
        </Alert.Content>
      </Alert>
    </>
  );
}
