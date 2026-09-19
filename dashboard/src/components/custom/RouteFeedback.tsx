import { Alert, Button, Card, Link } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { createLink, useRouter } from "@tanstack/react-router";
import { PageSkeleton } from "@/components/custom/PreviewLayout";

const RouterLink = createLink(Link);

export function RoutePending() {
  return (
    <Card aria-busy="true">
      <Card.Content>
        <div className="flex flex-col gap-4">
          <p role="status">
            <Trans id="route.loading">Loading page…</Trans>
          </p>
          <PageSkeleton />
        </div>
      </Card.Content>
    </Card>
  );
}

export function RouteError() {
  const router = useRouter();
  return (
    <Card>
      <Card.Content>
        <Alert status="danger" role="alert">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title render={(props) => <h1 {...props} />}>
              <Trans id="route.failed">Unable to load this page</Trans>
            </Alert.Title>
            <Alert.Description>
              <Trans id="route.retry.description">
                Check your connection and try again.
              </Trans>
            </Alert.Description>
          </Alert.Content>
        </Alert>
      </Card.Content>
      <Card.Footer>
        <Button
          onPress={() => {
            void router.invalidate();
          }}
        >
          <Trans id="route.retry">Try again</Trans>
        </Button>
      </Card.Footer>
    </Card>
  );
}

export function RouteNotFound() {
  return (
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h1 {...props} />}>
          <Trans id="route.not-found">Page not found</Trans>
        </Card.Title>
        <Card.Description>
          <Trans id="route.not-found.description">
            This address does not have a page.
          </Trans>
        </Card.Description>
      </Card.Header>
      <Card.Footer>
        <RouterLink to="/">
          <Trans id="route.home">Return to preview</Trans>
        </RouterLink>
      </Card.Footer>
    </Card>
  );
}
