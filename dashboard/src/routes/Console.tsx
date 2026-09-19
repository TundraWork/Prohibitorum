import { Alert, Card } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { sessionQueryOptions } from "@/api/queries";

export function Console() {
  const { data: session } = useQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });
  if (!session) return null;
  return (
    <>
      <h1 className="text-2xl font-semibold">
        <Trans id="console.home">Console home</Trans>
      </h1>
      <Card>
        <Card.Header>
          <Card.Title render={(props) => <h2 {...props} />}>
            <Trans id="console.current-account">Current account</Trans>
          </Card.Title>
        </Card.Header>
        <Card.Content>
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
        </Card.Content>
      </Card>
      <Alert status="accent">
        <Alert.Indicator />
        <Alert.Content>
          <Alert.Title>
            <Trans id="console.availability">
              Profile, security, applications, and devices will be available in
              a later update.
            </Trans>
          </Alert.Title>
        </Alert.Content>
      </Alert>
    </>
  );
}
