import { Button, Card, Link, Spinner } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { createLink, useRouter } from "@tanstack/react-router";
import { describeError } from "@/api/errors";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

const RouterLink = createLink(Link);

/** Pending state for public routes: the layout supplies the card, this only centers a spinner inside it. */
export function PublicPending() {
  return (
    <div className="flex justify-center py-16">
      <Spinner size="lg" />
    </div>
  );
}

export function RouteError({ error }: { error: unknown }) {
  const { i18n } = useLingui();
  const router = useRouter();
  return (
    <Card>
      <Card.Content>
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title render={(props) => <h1 {...props} />}>
              <Trans id="route.failed">Unable to load this page</Trans>
            </SurfaceAlert.Title>
            <SurfaceAlert.Description>
              {i18n._(describeError(error))}
            </SurfaceAlert.Description>
          </SurfaceAlert.Content>
        </SurfaceAlert>
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
        <RouterLink to="/preview/components">
          <Trans id="route.home">Return to preview</Trans>
        </RouterLink>
      </Card.Footer>
    </Card>
  );
}
