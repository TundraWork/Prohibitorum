import { Alert, Avatar, Chip, Dropdown, Label } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link2, Unlink } from "lucide-react";
import { useState } from "react";
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
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

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

  const unlink = useMutation(unlinkIdentityMutationOptions(queryClient));

  const available = providers.data ?? [];
  const linkedSlugs = new Set(
    (identities.data ?? []).map((identity) => identity.providerSlug),
  );

  const startLink = (slug: string) => {
    setError(null);
    setLinking(slug);
    void runWithSudo(async () => {
      // A full-page assignment: the response is a redirect the browser has to
      // follow, not a body the client can read.
      window.location.assign(identityLinkUrl(slug));
    }, sudoReason.linkIdentity)
      .catch((failure: unknown) => {
        if (!isCancellation(failure)) setError(failure);
      })
      .finally(() => setLinking(null));
  };

  return (
    <>
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-4">
          <LinkIdentityMenu
            providers={available}
            linkedSlugs={linkedSlugs}
            isPending={linking !== null}
            onSelect={startLink}
          />
        </div>

        {error !== null && (
          <Alert status="danger" role="alert">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Title>{t(describeError(error))}</Alert.Title>
            </Alert.Content>
          </Alert>
        )}

        <ItemList
          label={t({
            id: "security.identities.table",
            message: "Connected identities",
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
              icon={<Link2 size={18} aria-hidden="true" />}
              title={identity.providerDisplayName}
              badges={
                <Chip size="sm" variant="soft">
                  <span className="uppercase">{identity.protocol}</span>
                </Chip>
              }
              details={[
                identity.email || undefined,
                <span key="subject" className="font-mono">
                  {identity.subject}
                </span>,
              ]}
              actions={
                <Button
                  isIconOnly
                  size="sm"
                  variant="danger-soft"
                  aria-label={t({
                    id: "security.identities.unlink",
                    message: "Unlink",
                  })}
                  onPress={() => setTarget(identity)}
                >
                  <Unlink size={16} aria-hidden="true" />
                </Button>
              }
            />
          ))}
        </ItemList>
      </div>

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
          unlink.mutate(target.id, {
            onSuccess: () => setTarget(null),
            onError: (failure) => {
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
  const { t } = useLingui();
  const label = t({
    id: "security.identities.link.title",
    message: "Link another identity",
  });

  return (
    <Dropdown>
      <Button isPending={isPending} isDisabled={isPending}>
        <Trans id="security.identities.link.title">Link another identity</Trans>
      </Button>
      <Dropdown.Popover placement="bottom start">
        <Dropdown.Menu
          aria-label={label}
          onAction={(key) => onSelect(String(key))}
        >
          {providers.length === 0 ? (
            <Dropdown.Item
              id="unavailable"
              textValue={t({
                id: "security.identities.link.unavailable",
                message: "No identities available to link",
              })}
              isDisabled
            >
              <Label>
                <Trans id="security.identities.link.unavailable">
                  No identities available to link
                </Trans>
              </Label>
            </Dropdown.Item>
          ) : (
            providers.map((provider) => (
              <Dropdown.Item
                key={provider.slug}
                id={provider.slug}
                textValue={provider.displayName}
                isDisabled={linkedSlugs.has(provider.slug)}
              >
                <Avatar className="size-5 shrink-0">
                  {provider.iconUrl && (
                    <Avatar.Image src={provider.iconUrl} alt="" />
                  )}
                  <Avatar.Fallback>
                    <Link2 size={12} aria-hidden="true" />
                  </Avatar.Fallback>
                </Avatar>
                <Label>{provider.displayName}</Label>
              </Dropdown.Item>
            ))
          )}
        </Dropdown.Menu>
      </Dropdown.Popover>
    </Dropdown>
  );
}
