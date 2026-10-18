import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useNavigate, useRouter } from "@tanstack/react-router";
import { describeErrorLanding } from "@/api/errors";
import { sessionQueryOptions } from "@/api/queries";
import { followRedirect, isSitePath, sitePathOf } from "@/app/redirect";
import { AppStrip } from "@/components/custom/AppStrip";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";
import { ReferenceLine } from "@/components/custom/ReferenceLine";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { Route } from "@/routes/_public.error";

/**
 * A flow the server could not continue, in the shape of a public page that
 * failed to load: what happened, a reference an administrator can look up,
 * and one way out. `error_description` is never shown: the server does not
 * set it, and anyone can put words there.
 */
export function ErrorLandingPage() {
  const search = Route.useSearch();
  const { data: session } = useSuspenseQuery(sessionQueryOptions());
  const { name: instance } = useInstanceBranding();
  const { i18n } = useLingui();
  const router = useRouter();
  const navigate = useNavigate();

  const message = describeErrorLanding({
    code: search.error,
    reason: search.reason,
    federationName: search.federationName,
    instance,
  });
  const app = search.app;
  const ref = search.ref;
  // Two failures carry the address the flow started from, because running it
  // again is the one remedy: a transient authorize failure, and a denial an
  // administrator can lift while the user waits. Only an address the denial
  // could restart — an authorize request or a require-identity entry, in
  // either shape — turns the way out into a retry; every other code keeps the
  // go-back link a server-validated return_to offers.
  const retryTo = retryAddress(search.error, search.reason, search.return_to);
  const back =
    retryTo === undefined && isSitePath(search.return_to)
      ? search.return_to
      : undefined;
  const deniedRetry =
    retryTo !== undefined && search.reason === "app_access_denied";

  return (
    <PublicStep
      strip={
        app ? (
          <AppStrip
            lead={<Trans id="error_page.strip.lead">Signing in to</Trans>}
            name={app}
          />
        ) : undefined
      }
      title={
        search.reason === "app_access_denied" && app ? (
          <Trans id="error_page.title.denied">
            You don't have access to {app}
          </Trans>
        ) : (
          <Trans id="error_page.title">Unable to continue</Trans>
        )
      }
      actions={
        <PublicStep.Actions>
          {retryTo !== undefined ? (
            <Button
              onClick={() => void followRedirect(router, retryTo)}
              variant="outline"
              size="lg"
            >
              <Trans id="route.retry">Try again</Trans>
            </Button>
          ) : back ? (
            <Button
              onClick={() => followRedirect(router, back)}
              variant="outline"
              size="lg"
            >
              <Trans id="error_page.back">Go back</Trans>
            </Button>
          ) : session ? (
            <Button
              onClick={() => navigate({ to: "/" })}
              variant="outline"
              size="lg"
            >
              <Trans id="error_page.account">Go to my account</Trans>
            </Button>
          ) : (
            <Button
              onClick={() => navigate({ to: "/login" })}
              variant="outline"
              size="lg"
            >
              <Trans id="error_page.sign_in">Sign in</Trans>
            </Button>
          )}
        </PublicStep.Actions>
      }
    >
      <SurfaceAlert status="danger">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>{i18n._(message)}</SurfaceAlert.Title>
          {deniedRetry && (
            <SurfaceAlert.Description>
              <Trans id="error_page.denied.retry_hint">
                Access hasn't been granted yet. Once an administrator grants it,
                you can try again.
              </Trans>
            </SurfaceAlert.Description>
          )}
        </SurfaceAlert.Content>
      </SurfaceAlert>
      {ref && <ReferenceLine reference={ref} />}
    </PublicStep>
  );
}

/**
 * The address a denial or a transient authorize failure left to run again,
 * when the link carries one this site can offer. A denial names the flow it
 * stopped — an authorize request or a require-identity entry, either shape —
 * while a transient failure only re-runs an authorize request. Anything else
 * is not a re-run this page offers.
 */
function retryAddress(
  error: string | undefined,
  reason: string | undefined,
  returnTo: string | undefined,
): string | undefined {
  if (returnTo === undefined) return undefined;
  if (reason === "app_access_denied") {
    const path = sitePathOf(returnTo);
    if (path === undefined) return undefined;
    return path.startsWith("/oauth/authorize") ||
      path.startsWith("/api/prohibitorum/require-identity")
      ? path
      : undefined;
  }
  if (error === "server_error" && returnTo.startsWith("/oauth/authorize")) {
    return sitePathOf(returnTo);
  }
  return undefined;
}
