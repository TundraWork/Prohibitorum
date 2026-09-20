import { Button, Card, Spinner } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
} from "@/api/queries";
import { PageHeader, RewriteNotice } from "@/components/custom/PreviewLayout";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

export function ApiPreview() {
  const config = useSuspenseQuery(publicConfigQueryOptions());
  const status = useSuspenseQuery(authStatusQueryOptions());
  const refreshing = config.isFetching || status.isFetching;
  return (
    <>
      <PageHeader
        eyebrow={
          <Trans id="api.eyebrow">PHB-65 / M2 · Public API preview</Trans>
        }
        title={<Trans id="api.title">Public configuration</Trans>}
        description={
          <Trans id="api.description">
            This page reads the instance configuration and initialization status
            from the server.
          </Trans>
        }
      />
      <RewriteNotice />
      <Card>
        <Card.Header>
          <Card.Title>
            <Trans id="api.instance">Instance</Trans>
          </Card.Title>
        </Card.Header>
        <Card.Content className="flex flex-col gap-4">
          <dl className="grid gap-4 sm:grid-cols-2">
            <div className="min-w-0 space-y-1">
              <dt className="text-sm text-muted">
                <Trans id="api.name">Name</Trans>
              </dt>
              <dd className="break-words">{config.data.instanceName}</dd>
            </div>
            <div className="min-w-0 space-y-1">
              <dt className="text-sm text-muted">
                <Trans id="api.initialization">Initialization</Trans>
              </dt>
              <dd>
                {status.data.bootstrapped ? (
                  <Trans id="api.initialized">Initialized</Trans>
                ) : (
                  <Trans id="api.uninitialized">Not initialized</Trans>
                )}
              </dd>
            </div>
            <div className="min-w-0 space-y-1">
              <dt className="text-sm text-muted">
                <Trans id="api.maintenance">Maintenance</Trans>
              </dt>
              <dd>
                {config.data.maintenanceMode ? (
                  <Trans id="api.maintenance.active">Active</Trans>
                ) : (
                  <Trans id="api.maintenance.inactive">Inactive</Trans>
                )}
              </dd>
            </div>
            <div className="min-w-0 space-y-1">
              <dt className="text-sm text-muted">
                <Trans id="api.issuer">Authenticator issuer</Trans>
              </dt>
              <dd className="break-words">{config.data.totp.issuer}</dd>
            </div>
          </dl>
          {config.data.maintenanceMode && config.data.maintenanceMessage && (
            <SurfaceAlert status="warning">
              <SurfaceAlert.Indicator />
              <SurfaceAlert.Content>
                <SurfaceAlert.Title>
                  {config.data.maintenanceMessage}
                </SurfaceAlert.Title>
              </SurfaceAlert.Content>
            </SurfaceAlert>
          )}
        </Card.Content>
        <Card.Footer className="flex flex-col items-start gap-3">
          <Button
            variant="outline"
            isPending={refreshing}
            onPress={() => {
              void Promise.all([config.refetch(), status.refetch()]);
            }}
          >
            {({ isPending }) => (
              <>
                {isPending && <Spinner size="sm" color="current" />}
                {isPending ? (
                  <Trans id="api.refreshing">Refreshing…</Trans>
                ) : (
                  <Trans id="api.refresh">Refresh data</Trans>
                )}
              </>
            )}
          </Button>
          <p className="text-sm text-muted">
            <Trans id="api.public-note">
              Initialization does not indicate whether you are signed in.
            </Trans>
          </p>
        </Card.Footer>
      </Card>
    </>
  );
}
