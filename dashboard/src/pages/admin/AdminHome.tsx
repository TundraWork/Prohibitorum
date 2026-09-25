import { Alert } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { ConsoleCard } from "@/components/custom/ConsoleCard";

/**
 * The management area's landing page. Its sections are named in the sidebar,
 * so this says what the area is for rather than repeating them — and names the
 * ones still to come, so a reader does not go looking for federation settings
 * that are not built yet.
 */
export function AdminHome() {
  return (
    <>
      <ConsoleCard title={<Trans id="admin.home.title">Administration</Trans>}>
        <p className="text-sm text-muted">
          <Trans id="admin.home.summary">
            Users, user groups, invitations, logs and settings are managed from
            the sections in the sidebar.
          </Trans>
        </p>
      </ConsoleCard>
      <Alert status="accent">
        <Alert.Indicator />
        <Alert.Content>
          <Alert.Title>
            <Trans id="admin.home.availability">
              Federation and downstream applications are not available yet. This
              area will grow to cover them.
            </Trans>
          </Alert.Title>
        </Alert.Content>
      </Alert>
    </>
  );
}
