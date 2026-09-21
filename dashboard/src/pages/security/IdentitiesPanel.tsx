import {
  Alert,
  AlertDialog,
  Avatar,
  Button,
  Dropdown,
  Label,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link2 } from "lucide-react";
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
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
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
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const identities = useQuery(identitiesQueryOptions());
  const providers = useQuery(federationProvidersQueryOptions());
  const [target, setTarget] = useState<Identity | null>(null);
  const [error, setError] = useState<unknown>(null);
  const [linking, setLinking] = useState<string | null>(null);

  const unlink = useMutation(unlinkIdentityMutationOptions(queryClient));

  const format = (value: string) =>
    new Intl.DateTimeFormat(i18n.locale, {
      dateStyle: "medium",
      timeStyle: "short",
    }).format(new Date(value));

  const columns: readonly TableColumn<Identity>[] = [
    {
      id: "provider",
      header: <Trans id="security.identities.column.provider">Provider</Trans>,
      cell: (identity) => (
        <span className="wrap-anywhere font-medium">
          {identity.providerDisplayName}
        </span>
      ),
    },
    {
      id: "protocol",
      header: <Trans id="security.identities.column.protocol">Protocol</Trans>,
      cell: (identity) => (
        <span className="uppercase">{identity.protocol}</span>
      ),
    },
    {
      id: "subject",
      header: <Trans id="security.identities.column.subject">Identifier</Trans>,
      cell: (identity) => (
        <span className="wrap-anywhere font-mono text-xs">
          {identity.subject}
        </span>
      ),
    },
    {
      id: "email",
      header: <Trans id="security.identities.column.email">Email</Trans>,
      cell: (identity) =>
        identity.email ? (
          <span className="wrap-anywhere">{identity.email}</span>
        ) : (
          <span className="text-muted">—</span>
        ),
    },
    {
      id: "linkedAt",
      header: <Trans id="security.identities.column.linked">Linked</Trans>,
      cell: (identity) => format(identity.linkedAt),
    },
    {
      id: "actions",
      header: <Trans id="security.column.actions">Actions</Trans>,
      cell: (identity) => (
        <Button
          size="sm"
          variant="danger-soft"
          onPress={() => setTarget(identity)}
        >
          <Trans id="security.identities.unlink">Unlink</Trans>
        </Button>
      ),
      align: "end",
    },
  ];

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
        <div className="flex flex-wrap items-center justify-end gap-4">
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

        <DataTable
          label={t({
            id: "security.identities.table",
            message: "Connected identities",
          })}
          columns={columns}
          rows={identities.data ?? []}
          rowId={(identity) => identity.id}
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
        />
      </div>

      <AlertDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
      >
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>
                  <Trans id="security.identities.unlink.title">
                    Unlink this identity?
                  </Trans>
                </AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>
                <p>
                  <Trans id="security.identities.unlink.body">
                    You will no longer be able to sign in through this provider.
                    Your other sign-in methods are unaffected.
                  </Trans>
                </p>
              </AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  onPress={() => setTarget(null)}
                  isDisabled={unlink.isPending}
                >
                  <Trans id="security.cancel">Cancel</Trans>
                </Button>
                <Button
                  variant="danger"
                  isPending={unlink.isPending}
                  onPress={() => {
                    if (!target) return;
                    unlink.mutate(target.id, {
                      onSuccess: () => setTarget(null),
                      onError: (failure) => {
                        setError(failure);
                        setTarget(null);
                      },
                    });
                  }}
                >
                  <Trans id="security.identities.unlink.action">Unlink</Trans>
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
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
      <Dropdown.Popover placement="bottom end">
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
