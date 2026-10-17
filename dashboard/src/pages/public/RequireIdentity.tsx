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
    void runWithSudo(async () => {
      // A full-page assignment: the response is a redirect the browser has to
      // follow, not a body the client can read.
      loadDocument(identityLinkUrl(slug, `/require-identity/${ticket}`));
    }, sudoReason.linkIdentity)
      .catch((failure: unknown) => {
        if (!isCancellation(failure)) setError(failure);
      })
      .finally(() => setLinking(null));
  };

  const media = (
    <div aria-hidden="true" className="flex">
      <EntityAvatar
        className="size-12"
        iconUrl={request.client.iconUrl}
        fallback={app.slice(0, 1)}
      />
    </div>
  );
  const returnButton = (label: ReactNode) => (
    <Button
      variant="secondary"
      fullWidth
      isPending={leaving}
      isDisabled={switchAccount.isPending}
      onPress={returnToApp}
    >
      {label}
    </Button>
  );

  if (request.mismatch) {
    return (
      <PublicStep
        media={media}
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
              fullWidth
              isPending={switchAccount.isPending}
              isDisabled={leaving}
              onPress={() => switchAccount.mutate()}
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
        media={media}
        title={<Trans id="require-identity.done">Returning to {app}</Trans>}
      />
    );
  }

  return (
    <>
      <PublicStep
        media={media}
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
          className="flex flex-col gap-2"
        >
          {request.providers.map((provider) => {
            const name = provider.displayName;
            return (
              <li key={provider.slug} className="flex items-center gap-3">
                <EntityAvatar
                  className="size-9 shrink-0"
                  iconUrl={provider.iconUrl}
                  fallback={provider.displayName.slice(0, 1)}
                />
                <span className="min-w-0 flex-1 text-sm font-medium wrap-anywhere">
                  {provider.displayName}
                </span>
                <Button
                  size="sm"
                  variant="secondary"
                  className="shrink-0"
                  isPending={linking === provider.slug}
                  isDisabled={
                    leaving || (linking !== null && linking !== provider.slug)
                  }
                  aria-label={t({
                    id: "require-identity.verify.label",
                    message: `Verify with ${name}`,
                  })}
                  onPress={() => startLink(provider.slug)}
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
