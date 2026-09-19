import { Button, Skeleton } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { Link, useRouter } from "@tanstack/react-router";

export function RoutePending() {
  return (
    <section aria-busy="true" className="preview-card">
      <p role="status">
        <Trans id="route.loading">Loading page…</Trans>
      </p>
      <div aria-hidden="true" className="skeleton-example">
        <Skeleton className="skeleton-line" />
        <Skeleton className="skeleton-line skeleton-medium" />
        <Skeleton className="skeleton-line skeleton-short" />
      </div>
    </section>
  );
}

export function RouteError() {
  const router = useRouter();
  return (
    <section className="preview-card">
      <h1>
        <Trans id="route.failed">Unable to load this page</Trans>
      </h1>
      <p>
        <Trans id="route.retry.description">
          Check your connection and try again.
        </Trans>
      </p>
      <Button
        onPress={() => {
          void router.invalidate();
        }}
      >
        <Trans id="route.retry">Try again</Trans>
      </Button>
    </section>
  );
}

export function RouteNotFound() {
  return (
    <section className="preview-card">
      <h1>
        <Trans id="route.not-found">Page not found</Trans>
      </h1>
      <p>
        <Trans id="route.not-found.description">
          This address does not have a page.
        </Trans>
      </p>
      <Link to="/">
        <Trans id="route.home">Return to preview</Trans>
      </Link>
    </section>
  );
}
