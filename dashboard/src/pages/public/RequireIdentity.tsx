import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { type ReactNode, useEffect, useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import { identityLinkUrl } from "@/api/mutations";
import { requireIdentityRequestQueryOptions } from "@/api/queries";
import { runWithSudo } from "@/api/sudo";
import { sudoReason } from "@/api/sudo-reasons";
import { loadDocument } from "@/app/load-document";
import { followRedirect } from "@/app/redirect";
import { AppStrip } from "@/components/custom/AppStrip";
import { Button } from "@/components/custom/Button";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { PublicStep } from "@/components/custom/PublicStep";
import { SudoDialog } from "@/components/custom/SudoDialog";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { useSwitchAccount } from "@/pages/public/consent/useSwitchAccount";
import { Route } from "@/routes/_public.require-identity.$ticket";

/**
 * An application needs an identity from one of the providers it listed. The
 * page names the application and the providers, links the one the reader
 * picks, and comes back here; once any is linked it returns to the
 * application. Returning without verifying is always on offer: the
 * application decides what it does without the identity.
 *
 * Linking needs a fresh verification, as on the Connected identities panel, so
 * the page carries its own sudo prompt: it sits outside the console that
 * normally provides one.
 */
export function RequireIdentityPage() {
  const { ticket } = Route.useParams();
  const { data: request } = useSuspenseQuery(
    requireIdentityRequestQueryOptions(ticket),
  );
  const { t } = useLingui();
  const router = useRouter();
  const switchAccount = useSwitchAccount(request.entryUrl);
  const [linking, setLinking] = useState<string | null>(null);
  const [leaving, setLeaving] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const app = request.client.displayName;

  const returnToApp = () => {
    setLeaving(true);
    void followRedirect(router, request.redirectUri);
  };

  // Back from a link with the identity in place: nothing is left to do here.
  useEffect(() => {
    if (request.satisfied && !request.mismatch) {
      void followRedirect(router, request.redirectUri);
    }
  }, [request.satisfied, request.mismatch, request.redirectUri, router]);

  const startLink = (slug: string) => {
    setError(null);
    setLinking(slug);
    void runWithSudo(
      async () => {
        // A full-page assignment: the response is a redirect the browser has to
        // follow, not a body the client can read.
        loadDocument(identityLinkUrl(slug, `/require-identity/${ticket}`));
      },
      sudoReason.linkIdentity,
      { quiet: true },
    )
      .catch((failure: unknown) => {
        if (!isCancellation(failure)) setError(failure);
      })
      .finally(() => setLinking(null));
  };

  const strip = (
    <AppStrip
      lead={<Trans id="require-identity.strip.lead">Requested by</Trans>}
      name={app}
      host={request.client.host}
      iconUrl={request.client.iconUrl}
    />
  );
  const returnButton = (label: ReactNode) => (
    <Button
      pending={leaving}
      disabled={switchAccount.isPending}
      onClick={returnToApp}
      variant="outline"
      size="lg"
      className="w-full"
    >
      {label}
    </Button>
  );

  if (request.mismatch) {
    return (
      <PublicStep
        strip={strip}
        title={
          <Trans id="require-identity.mismatch.title">
            You're signed in as a different account
          </Trans>
        }
        description={
          <Trans id="require-identity.mismatch.description">
            {app} is using another account than the one signed in here. Sign in
            with that account to verify it, or go back to {app}.
          </Trans>
        }
        actions={
          <PublicStep.Actions>
            <Button
              pending={switchAccount.isPending}
              disabled={leaving}
              onClick={() => switchAccount.mutate()}
              size="lg"
              className="w-full"
            >
              <Trans id="require-identity.mismatch.switch">
                Sign out and use another account
              </Trans>
            </Button>
            {returnButton(
              <Trans id="require-identity.return">Return to {app}</Trans>,
            )}
          </PublicStep.Actions>
        }
      />
    );
  }

  if (request.satisfied) {
    return (
      <PublicStep
        strip={strip}
        title={<Trans id="require-identity.done">Returning to {app}</Trans>}
      />
    );
  }

  return (
    <>
      <PublicStep
        strip={strip}
        title={
          <Trans id="require-identity.title">
            {app} needs to verify your identity
          </Trans>
        }
        description={
          request.providers.length > 1 ? (
            <Trans id="require-identity.any">
              Verify with any one of these.
            </Trans>
          ) : undefined
        }
        actions={
          <PublicStep.Actions>
            {returnButton(
              <Trans id="require-identity.not-now">
                Not now, return to {app}
              </Trans>,
            )}
          </PublicStep.Actions>
        }
      >
        {error !== null && (
          <SurfaceAlert status="danger" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>{t(describeError(error))}</SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}
        <ul
          aria-label={t({
            id: "require-identity.providers",
            message: "Providers",
          })}
          className="divide-y overflow-hidden rounded-xl border bg-background shadow-xs/5"
        >
          {request.providers.map((provider) => {
            const name = provider.displayName;
            return (
              <li
                key={provider.slug}
                className="flex items-center gap-3 px-3.5 py-3"
              >
                <span aria-hidden="true" className="flex shrink-0">
                  <EntityAvatar
                    size="md"
                    iconUrl={provider.iconUrl}
                    name={name}
                  />
                </span>
                <span className="min-w-0 flex-1 font-medium text-sm wrap-anywhere">
                  {name}
                </span>
                <Button
                  pending={linking === provider.slug}
                  disabled={
                    leaving || (linking !== null && linking !== provider.slug)
                  }
                  aria-label={t({
                    id: "require-identity.verify.label",
                    message: `Verify with ${name}`,
                  })}
                  onClick={() => startLink(provider.slug)}
                  variant="outline"
                  size="sm"
                  className="shrink-0"
                >
                  <Trans id="require-identity.verify">Verify</Trans>
                </Button>
              </li>
            );
          })}
        </ul>
      </PublicStep>
      <SudoDialog />
    </>
  );
}
