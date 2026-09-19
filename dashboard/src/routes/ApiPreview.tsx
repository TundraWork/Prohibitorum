import { Button } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
} from "@/api/queries";

export function ApiPreview() {
  const config = useSuspenseQuery(publicConfigQueryOptions());
  const status = useSuspenseQuery(authStatusQueryOptions());
  const refreshing = config.isFetching || status.isFetching;
  return (
    <>
      <div className="eyebrow">
        <Trans id="api.eyebrow">PHB-65 / M2 · Public API preview</Trans>
      </div>
      <h1>
        <Trans id="api.title">Public configuration</Trans>
      </h1>
      <p className="intro">
        <Trans id="api.description">
          This page reads the instance configuration and initialization status
          from the server.
        </Trans>
      </p>
      <div className="rewrite-notice">
        <Trans id="preview.notice">
          The frontend is being rebuilt. Sign-in and other features are
          temporarily unavailable.
        </Trans>
      </div>
      <section className="preview-card">
        <h2>
          <Trans id="api.instance">Instance</Trans>
        </h2>
        <dl className="api-details">
          <div>
            <dt>
              <Trans id="api.name">Name</Trans>
            </dt>
            <dd>{config.data.instanceName}</dd>
          </div>
          <div>
            <dt>
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
          <div>
            <dt>
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
          <div>
            <dt>
              <Trans id="api.issuer">Authenticator issuer</Trans>
            </dt>
            <dd>{config.data.totp.issuer}</dd>
          </div>
        </dl>
        {config.data.maintenanceMode && config.data.maintenanceMessage && (
          <p>{config.data.maintenanceMessage}</p>
        )}
        <div className="actions">
          <Button
            variant="outline"
            isDisabled={refreshing}
            onPress={() => {
              void Promise.all([config.refetch(), status.refetch()]);
            }}
          >
            <Trans id="api.refresh">Refresh data</Trans>
          </Button>
          {refreshing && (
            <span role="status">
              <Trans id="api.refreshing">Refreshing…</Trans>
            </span>
          )}
        </div>
        <p>
          <Trans id="api.public-note">
            Initialization does not indicate whether you are signed in.
          </Trans>
        </p>
      </section>
    </>
  );
}
