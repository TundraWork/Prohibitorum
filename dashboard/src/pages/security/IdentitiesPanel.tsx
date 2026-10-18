import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link2, Unlink } from "lucide-react";
import { useEffect, useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  identityLinkUrl,
  unlinkIdentityMutationOptions,
} from "@/api/mutations";
import {
  federationProvidersQueryOptions,
  identitiesQueryOptions,
} from "@/api/queries";
import { runWithSudo } from "@/api/sudo";
import { sudoReason } from "@/api/sudo-reasons";
import { loadDocument } from "@/app/load-document";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import { Badge } from "@/components/ui/badge";
import { Menu, MenuItem, MenuPopup, MenuTrigger } from "@/components/ui/menu";

type Identity = components["schemas"]["AccountIdentityView"];
type Provider = components["schemas"]["FederationProvider"];

/**
 * Upstream identities bound to this account.
 *
 * Linking leaves the SPA: the backend answers the begin request with a redirect
 * to the provider and brings the browser back to a console route afterwards, so
 * the browser is sent there directly rather than through the router. The sudo
 * prompt runs first, because the begin endpoint is sudo-guarded and a redirect
 * cannot ask for verification.
 *
 * The providers are not listed on the page itself: the header action opens them
 * as a menu, each entry carrying the provider's mark and name, which keeps the
 * panel one list of what is actually linked.
 */
export function IdentitiesPanel() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const identities = useQuery(identitiesQueryOptions());
  const providers = useQuery(federationProvidersQueryOptions());
  const [target, setTarget] = useState<Identity | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [linking, setLinking] = useState<string | null>(null);

  // The failure is drawn in this section, so the toast stays silent.
  const unlink = useMutation(
    withInlineReport(unlinkIdentityMutationOptions(queryClient), true),
  );

  const available = providers.data ?? [];
  const linkedSlugs = new Set(
    (identities.data ?? []).map((identity) => identity.providerSlug),
  );

  // A press leaves the page and stays pending until it is gone; the browser
  // can show this page again from the back-forward cache with the state kept.
  useEffect(() => {
    const restore = (event: PageTransitionEvent) => {
      if (event.persisted) setLinking(null);
    };
    window.addEventListener("pageshow", restore);
    return () => window.removeEventListener("pageshow", restore);
  }, []);

  const startLink = (slug: string) => {
    setError(null);
    setLinking(slug);
    runWithSudo(async () => {
      // A full-page load: the response is a redirect the browser has to
      // follow, not a body the client can read.
      loadDocument(identityLinkUrl(slug));
    }, sudoReason.linkIdentity).catch((failure: unknown) => {
      setLinking(null);
      if (!isCancellation(failure)) setError(failure);
    });
  };

  return (
    <>
      <Section
        icon={<Link2 />}
        title={
          <Trans id="security.section.identities">Linked identities</Trans>
        }
        description={
          <Trans id="security.identities.description">
            Other accounts you can sign in with. Unlinking one leaves your other
            ways in as they are.
          </Trans>
        }
        action={
          <LinkIdentityMenu
            providers={available}
            linkedSlugs={linkedSlugs}
            isPending={linking !== null}
            onSelect={startLink}
          />
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

        <ItemList
          label={t({
            id: "security.identities.table",
            message: "Linked identities",
          })}
          loading={identities.isPending}
          empty={
            <TableEmptyState
              icon={<Link2 size={18} strokeWidth={1.75} aria-hidden="true" />}
              title={
                <Trans id="security.identities.empty">
                  No other identities linked yet
                </Trans>
              }
            />
          }
        >
          {(identities.data ?? []).map((identity) => (
            <ItemListRow
              key={identity.id}
              icon={
                <EntityAvatar
                  size="md"
                  name={identity.providerDisplayName}
                  iconUrl={
                    available.find(
                      (provider) => provider.slug === identity.providerSlug,
                    )?.iconUrl
                  }
                />
              }
              title={identity.providerDisplayName}
              badge={
                <Badge variant="outline">
                  {identity.protocol.toUpperCase()}
                </Badge>
              }
              meta={
                <Trans id="security.identities.meta.linked">
                  Linked <RelativeTime value={identity.linkedAt} />
                </Trans>
              }
              details={[
                identity.email || undefined,
                <span key="subject" className="font-mono wrap-anywhere">
                  {identity.subject}
                </span>,
              ]}
              actions={
                <Button
                  aria-label={t({
                    id: "security.identities.unlink",
                    message: "Unlink",
                  })}
                  onClick={() => setTarget(identity)}
                  variant="destructive-outline"
                  size="icon-sm"
                >
                  <Unlink aria-hidden="true" />
                </Button>
              }
            />
          ))}
        </ItemList>
      </Section>

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
        status="danger"
        title={
          <Trans id="security.identities.unlink.title">
            Unlink this identity?
          </Trans>
        }
        body={
          <p>
            <Trans id="security.identities.unlink.body">
              You will no longer be able to sign in through this provider. Your
              other sign-in methods are unaffected.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="security.identities.unlink.action">Unlink</Trans>
        }
        isPending={unlink.isPending}
        onConfirm={() => {
          if (!target) return;
          setError(null);
          unlink.mutate(target.id, {
            onSuccess: () => setTarget(null),
            onError: (failure) => {
              // Backing out of the sudo prompt returns to this question.
              if (isCancellation(failure)) return;
              setError(failure);
              setTarget(null);
            },
          });
        }}
      />
    </>
  );
}

/**
 * The configured providers, as a menu under the panel's primary action. Each
 * entry carries the provider's own mark so a member recognises the service
 * before committing to the sign-in round trip.
 *
 * The menu always opens, even when there is nothing to pick: an instance with no
 * providers answers with one disabled line, and a provider this account already
 * carries stays in the list, disabled, so the menu still tells the member what
 * exists rather than losing the entry.
 */
function LinkIdentityMenu({
  providers,
  linkedSlugs,
  isPending,
  onSelect,
}: {
  providers: readonly Provider[];
  linkedSlugs: ReadonlySet<string>;
  isPending: boolean;
  onSelect: (slug: string) => void;
}) {
  return (
    <Menu disabled={isPending}>
      <MenuTrigger
        render={<Button variant="outline" size="sm" pending={isPending} />}
      >
        <Trans id="security.identities.link.title">Link another identity</Trans>
      </MenuTrigger>
      <MenuPopup
        align="end"
        portalProps={{ container: getTopLayerContainer() }}
      >
        {providers.length === 0 ? (
          <MenuItem disabled>
            <Trans id="security.identities.link.unavailable">
              No identities available to link
            </Trans>
          </MenuItem>
        ) : (
          providers.map((provider) => {
            const linked = linkedSlugs.has(provider.slug);
            return (
              <MenuItem
                key={provider.slug}
                disabled={linked}
                onClick={() => onSelect(provider.slug)}
              >
                {/* The initials stand in for a missing icon, not for the name. */}
                <span aria-hidden="true" className="contents">
                  <EntityAvatar
                    size="sm"
                    name={provider.displayName}
                    iconUrl={provider.iconUrl}
                  />
                </span>
                {provider.displayName}
                {linked && (
                  <span className="ms-auto ps-4 text-muted-foreground text-xs">
                    <Trans id="security.identities.link.linked">Linked</Trans>
                  </span>
                )}
              </MenuItem>
            );
          })
        )}
      </MenuPopup>
    </Menu>
  );
}
