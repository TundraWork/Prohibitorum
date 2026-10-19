import { Alert, Chip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  Fingerprint,
  KeyRound,
  Link2,
  MonitorSmartphone,
  Ticket,
} from "lucide-react";
import { describeError } from "@/api/errors";
import {
  deleteAccountCredentialMutationOptions,
  revokeAccountSessionMutationOptions,
  revokeAccountSessionsMutationOptions,
  revokeAccountTokenMutationOptions,
} from "@/api/mutations";
import {
  accountCredentialsQueryOptions,
  accountIdentitiesQueryOptions,
  accountSessionsQueryOptions,
  accountTokensQueryOptions,
} from "@/api/queries";
import { DangerZone } from "@/components/custom/DangerZone";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import {
  TokenAccessBadge,
  TokenAccessSummary,
} from "@/components/custom/TokenAccess";

/**
 * Everything about how this account gets in, and the ways to cut it off: one
 * list per kind of sign-in, all on screen at once, so an admin reads the whole
 * picture without opening anything.
 */
export function AccountAccessLegacy({ accountId }: { accountId: number }) {
  return (
    <div
      id="access"
      className="flex scroll-mt-[calc(var(--app-sticky-offset)+5rem)] flex-col gap-4"
    >
      <IdentitiesBlock accountId={accountId} />
      <PasskeysBlock accountId={accountId} />
      <SessionsBlock accountId={accountId} />
      <TokensBlock accountId={accountId} />
    </div>
  );
}

/**
 * A failed block reports itself rather than leaving an empty list behind.
 * Above a list it sits on the page; inside a row it is on the card's surface.
 */
function BlockError({
  error,
  onSurface = false,
}: {
  error: unknown;
  onSurface?: boolean;
}) {
  const { t } = useLingui();
  const Notice = onSurface ? SurfaceAlert : Alert;
  return (
    <Notice status="danger" role="alert">
      <Notice.Indicator />
      <Notice.Content>
        <Notice.Title>{t(describeError(error))}</Notice.Title>
      </Notice.Content>
    </Notice>
  );
}

function useDateFormat() {
  const { i18n } = useLingui();
  return (value?: string) =>
    value === undefined
      ? null
      : new Intl.DateTimeFormat(i18n.locale, {
          dateStyle: "medium",
        }).format(new Date(value));
}

function IdentitiesBlock({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const identities = useQuery(accountIdentitiesQueryOptions(accountId));

  return (
    <div className="flex flex-col gap-2">
      {identities.error !== null && <BlockError error={identities.error} />}
      <ItemList
        title={
          <Trans id="admin.user.identities.title">Connected identities</Trans>
        }
        label={t({
          id: "admin.user.identities.table",
          message: "Connected identities",
        })}
        loading={identities.isPending}
        empty={
          <TableEmptyState
            icon={<Link2 size={18} strokeWidth={1.75} aria-hidden="true" />}
            title={
              <Trans id="admin.user.identities.empty">
                No connected identities
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
              <span key="subject" className="font-mono">
                {identity.subject}
              </span>,
            ]}
          />
        ))}
      </ItemList>
    </div>
  );
}

function PasskeysBlock({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const credentials = useQuery(accountCredentialsQueryOptions(accountId));
  const revoke = useMutation(
    deleteAccountCredentialMutationOptions(queryClient),
  );

  return (
    <div className="flex flex-col gap-2">
      {credentials.error !== null && <BlockError error={credentials.error} />}
      {revoke.error !== null && <BlockError error={revoke.error} />}
      <ItemList
        title={<Trans id="admin.user.passkeys.title">Passkeys</Trans>}
        label={t({ id: "admin.user.passkeys.table", message: "Passkeys" })}
        loading={credentials.isPending}
        empty={
          <TableEmptyState
            icon={
              <Fingerprint size={18} strokeWidth={1.75} aria-hidden="true" />
            }
            title={<Trans id="admin.user.passkeys.empty">No passkeys</Trans>}
          />
        }
      >
        {(credentials.data?.items ?? []).map((credential) => {
          const lastUsed = credential.lastUsedAt;
          return (
            <ItemListRow
              key={credential.id}
              icon={<KeyRound size={18} aria-hidden="true" />}
              title={
                credential.nickname || (
                  <span className="text-muted">
                    <Trans id="admin.user.passkeys.unnamed">
                      Unnamed passkey
                    </Trans>
                  </span>
                )
              }
              details={[
                <Trans key="added" id="admin.user.passkeys.detail.added">
                  Added <RelativeTime value={credential.createdAt} />
                </Trans>,
                lastUsed === undefined ? (
                  <Trans key="used" id="admin.user.passkeys.never_used">
                    Not used yet
                  </Trans>
                ) : (
                  <Trans key="used" id="admin.user.passkeys.detail.lastUsed">
                    Last used <RelativeTime value={lastUsed} />
                  </Trans>
                ),
                <span key="suffix" className="font-mono">
                  …{credential.credentialIdSuffix}
                </span>,
              ]}
              actions={
                <DangerZone
                  size="sm"
                  label={<Trans id="admin.user.passkeys.revoke">Remove</Trans>}
                  title={
                    <Trans id="admin.user.passkeys.revoke.title">
                      Remove this passkey?
                    </Trans>
                  }
                  body={
                    <p>
                      <Trans id="admin.user.passkeys.revoke.body">
                        This passkey stops working immediately. If it is the
                        only way this account signs in, they will need a new
                        registration link.
                      </Trans>
                    </p>
                  }
                  confirmLabel={
                    <Trans id="admin.user.passkeys.revoke.action">
                      Remove passkey
                    </Trans>
                  }
                  isPending={revoke.isPending}
                  onConfirm={() =>
                    revoke.mutate({ accountId, credentialId: credential.id })
                  }
                />
              }
            />
          );
        })}
      </ItemList>
    </div>
  );
}

/** A one-line summary of a User-Agent; the raw string is not parsed further. */
function agentSummary(value?: string): string {
  if (!value) return "—";
  return value.length > 80 ? `${value.slice(0, 79)}…` : value;
}

function SessionsBlock({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const sessions = useQuery(accountSessionsQueryOptions(accountId));
  const revoke = useMutation(revokeAccountSessionMutationOptions(queryClient));
  const revokeAll = useMutation(
    revokeAccountSessionsMutationOptions(queryClient),
  );
  const rows = sessions.data?.items ?? [];

  return (
    <div className="flex flex-col gap-2">
      {sessions.error !== null && <BlockError error={sessions.error} />}
      {revoke.error !== null && <BlockError error={revoke.error} />}
      {revokeAll.error !== null && <BlockError error={revokeAll.error} />}
      <ItemList
        title={<Trans id="admin.user.sessions.title">Active sessions</Trans>}
        label={t({
          id: "admin.user.sessions.table",
          message: "Active sessions",
        })}
        loading={sessions.isPending}
        empty={
          <TableEmptyState
            icon={
              <MonitorSmartphone
                size={18}
                strokeWidth={1.75}
                aria-hidden="true"
              />
            }
            title={
              <Trans id="admin.user.sessions.empty">No active sessions</Trans>
            }
          />
        }
        footer={
          rows.length > 1 ? (
            <DangerZone
              size="sm"
              label={
                <Trans id="admin.user.sessions.revoke_all">
                  Sign out everywhere
                </Trans>
              }
              title={
                <Trans id="admin.user.sessions.revoke_all.title">
                  Sign this account out everywhere?
                </Trans>
              }
              body={
                <p>
                  <Trans id="admin.user.sessions.revoke_all.body">
                    Every device signed in to this account is signed out
                    immediately and will need to sign in again.
                  </Trans>
                </p>
              }
              confirmLabel={
                <Trans id="admin.user.sessions.revoke_all.action">
                  Sign out everywhere
                </Trans>
              }
              isPending={revokeAll.isPending}
              onConfirm={() => revokeAll.mutate(accountId)}
            />
          ) : undefined
        }
      >
        {rows.map((session) => (
          <ItemListRow
            key={session.id}
            icon={<MonitorSmartphone size={18} aria-hidden="true" />}
            title={
              <span title={session.userAgent ?? undefined}>
                {agentSummary(session.userAgent)}
              </span>
            }
            details={[
              session.lastSeenIp || undefined,
              <Trans key="issued" id="admin.user.sessions.detail.issued">
                Started <RelativeTime value={session.issuedAt} />
              </Trans>,
            ]}
            actions={
              <DangerZone
                size="sm"
                label={<Trans id="admin.user.sessions.revoke">End</Trans>}
                title={
                  <Trans id="admin.user.sessions.revoke.title">
                    End this session?
                  </Trans>
                }
                body={
                  <p>
                    <Trans id="admin.user.sessions.revoke.body">
                      That device is signed out immediately and will need to
                      sign in again. Anything it is doing right now stops.
                    </Trans>
                  </p>
                }
                confirmLabel={
                  <Trans id="admin.user.sessions.revoke.action">
                    End session
                  </Trans>
                }
                isPending={revoke.isPending}
                onConfirm={() => revoke.mutate({ accountId, id: session.id })}
              />
            }
          />
        ))}
      </ItemList>
    </div>
  );
}

function TokensBlock({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const format = useDateFormat();
  const queryClient = useQueryClient();
  const tokens = useQuery(accountTokensQueryOptions(accountId));
  const revoke = useMutation(revokeAccountTokenMutationOptions(queryClient));

  return (
    <div className="flex flex-col gap-2">
      {tokens.error !== null && <BlockError error={tokens.error} />}
      {revoke.error !== null && <BlockError error={revoke.error} />}
      <ItemList
        title={<Trans id="admin.user.tokens.title">Access tokens</Trans>}
        label={t({ id: "admin.user.tokens.table", message: "Access tokens" })}
        loading={tokens.isPending}
        empty={
          <TableEmptyState
            icon={<Ticket size={18} strokeWidth={1.75} aria-hidden="true" />}
            title={<Trans id="admin.user.tokens.empty">No access tokens</Trans>}
          />
        }
      >
        {(tokens.data?.items ?? []).map((token) => {
          const expires = format(token.expiresAt);
          const lastUsed = token.lastUsedAt;
          const name = token.name;
          return (
            <ItemListRow
              key={token.id}
              icon={<Ticket size={18} aria-hidden="true" />}
              title={name}
              badges={<TokenAccessBadge token={token} />}
              details={[
                <span key="hint" className="font-mono">
                  {token.tokenHint}
                </span>,
                <TokenAccessSummary key="access" token={token} />,
                expires === null ? (
                  <Trans key="expires" id="admin.user.tokens.detail.noExpiry">
                    Never expires
                  </Trans>
                ) : (
                  <Trans key="expires" id="admin.user.tokens.detail.expires">
                    Expires {expires}
                  </Trans>
                ),
                lastUsed === undefined ? (
                  <Trans key="used" id="admin.user.tokens.never_used">
                    Not used yet
                  </Trans>
                ) : (
                  <Trans key="used" id="admin.user.tokens.detail.lastUsed">
                    Last used <RelativeTime value={lastUsed} />
                  </Trans>
                ),
              ]}
              actions={
                <DangerZone
                  size="sm"
                  label={<Trans id="admin.user.tokens.revoke">Revoke</Trans>}
                  accessibleLabel={t({
                    id: "admin.user.tokens.revoke.named",
                    message: `Revoke ${name}`,
                  })}
                  title={
                    <Trans id="admin.user.tokens.revoke.title">
                      Revoke this token?
                    </Trans>
                  }
                  body={
                    <p>
                      <Trans id="admin.user.tokens.revoke.body">
                        Anything using this token stops working immediately.
                        This cannot be undone.
                      </Trans>
                    </p>
                  }
                  confirmLabel={
                    <Trans id="admin.user.tokens.revoke.action">
                      Revoke token
                    </Trans>
                  }
                  isPending={revoke.isPending}
                  onConfirm={() => revoke.mutate({ accountId, id: token.id })}
                />
              }
            />
          );
        })}
      </ItemList>
    </div>
  );
}
