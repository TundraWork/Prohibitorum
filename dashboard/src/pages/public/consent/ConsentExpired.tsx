import { Trans } from "@lingui/react/macro";
import { useRouter } from "@tanstack/react-router";
import { followRedirect, sitePathOf } from "@/app/redirect";
import { Button } from "@/components/custom/Button";
import { PublicStep } from "@/components/custom/PublicStep";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/**
 * The loader's signal that a consent page's ticket is gone and the
 * authorization it belongs to can be started again: `retryTo` is the
 * authorization address, when the link carried one.
 */
export class ConsentExpiredError extends Error {
  constructor(readonly retryTo: string) {
    super("consent ticket expired");
  }
}

/**
 * What an OIDC consent page shows when its ticket is no longer usable: the
 * request can only be answered once, and starting it again from the
 * application's original authorization address is the one way on. That
 * re-runs the whole authorization, so anything wrong with the application's
 * request itself is caught there, not here. Without an authorization address
 * this site can offer, there is nothing to do but say what happened.
 */
export function ConsentExpired({ retryTo }: { retryTo: string }) {
  const router = useRouter();
  const restart = sitePathOf(retryTo);
  return (
    <PublicStep title={<Trans id="error_page.title">Unable to continue</Trans>}>
      <SurfaceAlert status="danger">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>
            <Trans id="consent.expired">
              This authorization request has expired or was already used.
            </Trans>
          </SurfaceAlert.Title>
          {restart !== undefined && (
            <div className="mt-3 flex flex-wrap gap-2">
              <Button
                size="sm"
                variant="secondary"
                onPress={() => void followRedirect(router, restart)}
              >
                <Trans id="route.retry">Try again</Trans>
              </Button>
            </div>
          )}
        </SurfaceAlert.Content>
      </SurfaceAlert>
    </PublicStep>
  );
}
