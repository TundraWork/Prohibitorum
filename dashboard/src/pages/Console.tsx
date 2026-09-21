import { Alert } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { sessionQueryOptions } from "@/api/queries";
import { ConsoleCard } from "@/components/custom/ConsoleCard";

export function Console() {
  const { data: session } = useSuspenseQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });
  if (session === null) return null;
  return (
    <>
      <ConsoleCard
        title={<Trans id="console.current-account">Current account</Trans>}
      >
        <dl className="grid min-w-0 gap-4 sm:grid-cols-2">
          <div className="min-w-0">
            <dt className="text-sm text-muted">
              <Trans id="console.display-name">Display name</Trans>
            </dt>
            <dd className="wrap-anywhere">{session.displayName}</dd>
          </div>
          <div className="min-w-0">
            <dt className="text-sm text-muted">
              <Trans id="console.username">Username</Trans>
            </dt>
            <dd className="wrap-anywhere">{session.username}</dd>
          </div>
        </dl>
      </ConsoleCard>
      <Alert status="accent">
        <Alert.Indicator />
        <Alert.Content>
          <Alert.Title>
            <Trans id="console.availability">
              Your profile, security settings, connected applications and
              devices are on the pages listed in the sidebar. Administrative
              pages are still to come.
            </Trans>
          </Alert.Title>
        </Alert.Content>
      </Alert>
    </>
  );
}
