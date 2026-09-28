import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useNavigate, useRouter } from "@tanstack/react-router";
import { describeErrorLanding } from "@/api/errors";
import { sessionQueryOptions } from "@/api/queries";
import { followRedirect } from "@/app/redirect";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { Route } from "@/routes/_public.error";

/**
 * A path on this site. A value starting `//` or `/\` is read by the browser
 * as another host, so it is not one.
 */
export function isSitePath(value: string | undefined): value is string {
  return (
    value?.startsWith("/") === true &&
    !value.startsWith("//") &&
    !value.startsWith("/\\")
  );
}

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
  const back = isSitePath(search.return_to) ? search.return_to : undefined;

  return (
    <PublicStep
      title={
        search.reason === "app_access_denied" && app ? (
          <Trans id="error_page.title.denied">
            You don't have access to {app}
          </Trans>
        ) : (
          <Trans id="error_page.title">Unable to continue</Trans>
        )
      }
    >
      <SurfaceAlert status="danger">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>{i18n._(message)}</SurfaceAlert.Title>
          {ref && (
            <SurfaceAlert.Description>
              <Trans id="error_page.reference">
                Reference{" "}
                <span className="select-all font-mono wrap-anywhere">
                  {ref}
                </span>
              </Trans>
            </SurfaceAlert.Description>
          )}
          <div className="mt-3 flex flex-wrap gap-2">
            {back ? (
              <Button
                size="sm"
                variant="secondary"
                onPress={() => followRedirect(router, back)}
              >
                <Trans id="error_page.back">Go back</Trans>
              </Button>
            ) : session ? (
              <Button
                size="sm"
                variant="secondary"
                onPress={() => navigate({ to: "/" })}
              >
                <Trans id="error_page.account">Go to my account</Trans>
              </Button>
            ) : (
              <Button
                size="sm"
                variant="secondary"
                onPress={() => navigate({ to: "/login" })}
              >
                <Trans id="error_page.sign_in">Sign in</Trans>
              </Button>
            )}
          </div>
        </SurfaceAlert.Content>
      </SurfaceAlert>
    </PublicStep>
  );
}
