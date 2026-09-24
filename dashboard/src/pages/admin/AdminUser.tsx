import {
  Alert,
  Chip,
  Description,
  Label,
  ListBox,
  Modal,
  Select,
  Tabs,
} from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  Fingerprint,
  KeyRound,
  Link2,
  MonitorSmartphone,
  Ticket,
} from "lucide-react";
import { useState } from "react";
import { describeError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  deleteAccountCredentialMutationOptions,
  deleteAccountMutationOptions,
  reissueEnrollmentMutationOptions,
  revokeAccountSessionMutationOptions,
  revokeAccountSessionsMutationOptions,
  revokeAccountTokenMutationOptions,
  setAccountDisabledMutationOptions,
  type UpdateAccountInput,
  updateAccountMutationOptions,
} from "@/api/mutations";
import {
  accountCredentialsQueryOptions,
  accountIdentitiesQueryOptions,
  accountQueryOptions,
  accountSessionsQueryOptions,
  accountTokensQueryOptions,
} from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { DangerZone } from "@/components/custom/DangerZone";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { SecretReveal } from "@/components/custom/SecretReveal";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { invitationLinkCopy } from "@/components/custom/secret-reveal-copy";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import type { AccountTab } from "@/pages/console/tabs";
import { Route } from "@/routes/_protected.admin.users_.$id";

type Account = components["schemas"]["AccountView"];

const attributesInvalid = msg({
  id: "admin.user.attributes.invalid",
  message: "This is not valid JSON. Check the braces, commas and quotes.",
});
const attributesNotObject = msg({
  id: "admin.user.attributes.not_object",
  message: "Attributes must be a JSON object, such as {}.",
});
const displayNameRequired = msg({
  id: "admin.user.displayName.required",
  message: "Give the account a display name.",
});

/**
 * Parses the attributes box. An empty box is an empty map rather than an error:
 * `PUT /accounts/{id}` replaces the whole record, so the form always has a map
 * to send, and "no attributes" has to be expressible.
 */
function parseAttributes(
  value: string,
):
  | { ok: true; value: Record<string, unknown> }
  | { ok: false; empty: boolean } {
  const trimmed = value.trim();
  if (trimmed === "") return { ok: true, value: {} };
  let parsed: unknown;
  try {
    parsed = JSON.parse(trimmed);
  } catch {
    return { ok: false, empty: false };
  }
  if (typeof parsed !== "object" || parsed === null || Array.isArray(parsed)) {
    return { ok: false, empty: true };
  }
  return { ok: true, value: parsed as Record<string, unknown> };
}

/** The stored map as the box shows it; an empty map stays an empty box. */
function formatAttributes(attributes?: Record<string, unknown>): string {
  if (!attributes || Object.keys(attributes).length === 0) return "";
  return JSON.stringify(attributes, null, 2);
}

/**
 * One account in the management area.
 *
 * The three tabs separate the fields an admin came to edit from the actions
 * that end someone's access: `profile` is a single form, `access` is everything
 * read-only about how the account gets in with its force-revokes, and `danger`
 * holds what cannot be undone.
 *
 * Every block on `access` and `danger` owns its query and its mutation, so one
 * failing list does not take the others down with it.
 */
export function AdminUser() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const { id } = Route.useParams();
  const { tab } = Route.useSearch();
  const accountId = Number(id);
  const account = useQuery(accountQueryOptions(accountId));

  // No loader stands behind this route, so there is no `pendingComponent` to
  // fall back to; the tabs wait rather than drawing a form with no values.
  if (account.isPending) return null;

  if (!account.data) {
    return (
      <ConsoleCard title={<Trans id="admin.user.title">Account</Trans>}>
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              {t(describeError(account.error))}
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      </ConsoleCard>
    );
  }

  return (
    <Tabs
      selectedKey={tab}
      onSelectionChange={(key) => {
        // replace, so browsing tabs never buries the page the user came from.
        void navigate({
          to: "/admin/users/$id",
          params: { id },
          search: { tab: key as AccountTab },
          replace: true,
        });
      }}
    >
      <Tabs.ListContainer className="ml-2 w-fit max-w-full">
        <Tabs.List
          aria-label={t({ id: "admin.user.tabs", message: "Account" })}
        >
          <Tabs.Tab className="whitespace-nowrap" id="profile">
            <Trans id="admin.user.tab.profile">Profile</Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
          <Tabs.Tab className="whitespace-nowrap" id="access">
            <Trans id="admin.user.tab.access">Access</Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
          <Tabs.Tab className="whitespace-nowrap" id="danger">
            <Trans id="admin.user.tab.danger">Danger zone</Trans>
            <Tabs.Indicator />
          </Tabs.Tab>
        </Tabs.List>
      </Tabs.ListContainer>
      <Tabs.Panel id="profile" className="pt-4">
        {tab === "profile" && <ProfilePanel account={account.data} />}
      </Tabs.Panel>
      <Tabs.Panel id="access" className="pt-4">
        {tab === "access" && <AccessPanel accountId={accountId} />}
      </Tabs.Panel>
      <Tabs.Panel id="danger" className="pt-4">
        {tab === "danger" && <DangerPanel account={account.data} />}
      </Tabs.Panel>
    </Tabs>
  );
}

/**
 * The account's own fields.
 *
 * `PUT /accounts/{id}` replaces the record rather than patching it, so an
 * omitted `attributes` clears whatever the account had. The box therefore
 * always submits the full map, and invalid JSON is refused here instead of
 * being sent as an empty one.
 */
function ProfilePanel({ account }: { account: Account }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const [saved, setSaved] = useState(false);
  const [role, setRole] = useState(account.role);
  const update = useMutation(updateAccountMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: {
      displayName: account.displayName,
      email: account.email ?? "",
      disabled: account.disabled,
      attributes: formatAttributes(account.attributes),
    },
    onSubmit: async ({ value }) => {
      setSaved(false);
      const attributes = parseAttributes(value.attributes);
      if (!attributes.ok) {
        form.setFieldMeta("attributes", (meta) => ({
          ...meta,
          errorMap: {
            ...meta.errorMap,
            onSubmit: attributes.empty
              ? attributesNotObject
              : attributesInvalid,
          },
        }));
        return;
      }
      const email = value.email.trim();
      const body: UpdateAccountInput = {
        displayName: value.displayName.trim(),
        disabled: value.disabled,
        role,
        attributes: attributes.value,
        ...(email === "" ? {} : { email }),
      };
      try {
        await update.mutateAsync({ id: account.id, body });
        setSaved(true);
      } catch (error) {
        applyServerError(form, error, {
          locations: {
            displayName: "displayName",
            email: "email",
            attributes: "attributes",
          },
          codes: { invalid_role: "displayName" },
        });
      }
    },
  });

  return (
    <ConsoleCard title={<Trans id="admin.user.profile.title">Profile</Trans>}>
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.user.profile.form",
            message: "Account profile",
          })}
        >
          <form.FormError />
          <form.AppField
            name="displayName"
            validators={{
              onBlur: ({ value }) =>
                value.trim() === "" ? displayNameRequired : undefined,
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="admin.user.displayName">Display name</Trans>}
                autoComplete="off"
                variant="secondary"
              />
            )}
          </form.AppField>
          <form.AppField name="email">
            {(field) => (
              <field.FormField
                label={<Trans id="admin.user.email">Email</Trans>}
                description={
                  <Trans id="admin.user.email.hint">
                    Optional. Leave it empty to store no address.
                  </Trans>
                }
                type="email"
                autoComplete="off"
                variant="secondary"
              />
            )}
          </form.AppField>

          <Select
            variant="secondary"
            value={role}
            onChange={(key) => {
              if (typeof key === "string") setRole(key);
            }}
          >
            <Label>
              <Trans id="admin.user.role">Role</Trans>
            </Label>
            <Select.Trigger>
              <Select.Value />
              <Select.Indicator />
            </Select.Trigger>
            <Select.Popover>
              <ListBox>
                <ListBox.Item
                  id="user"
                  textValue={t({ id: "admin.user.role.user", message: "User" })}
                >
                  <Label>
                    <Trans id="admin.user.role.user">User</Trans>
                  </Label>
                  <ListBox.ItemIndicator />
                </ListBox.Item>
                <ListBox.Item
                  id="admin"
                  textValue={t({
                    id: "admin.user.role.admin",
                    message: "Admin",
                  })}
                >
                  <Label>
                    <Trans id="admin.user.role.admin">Admin</Trans>
                  </Label>
                  <ListBox.ItemIndicator />
                </ListBox.Item>
              </ListBox>
            </Select.Popover>
          </Select>

          <form.AppField name="disabled">
            {(field) => (
              <field.SwitchField
                label={<Trans id="admin.user.disabled">Disabled</Trans>}
                description={
                  <Trans id="admin.user.disabled.hint">
                    A disabled account cannot sign in. An admin has to be made a
                    user before they can be disabled.
                  </Trans>
                }
              />
            )}
          </form.AppField>

          <form.AppField
            name="attributes"
            validators={{
              onBlur: ({ value }) => {
                const parsed = parseAttributes(value);
                if (parsed.ok) return undefined;
                return parsed.empty ? attributesNotObject : attributesInvalid;
              },
            }}
          >
            {(field) => (
              <field.TextAreaField
                label={<Trans id="admin.user.attributes">Attributes</Trans>}
                description={
                  <Trans id="admin.user.attributes.hint">
                    A JSON object stored with the account. Saving sends whatever
                    is in this box, so anything removed here is removed from the
                    account.
                  </Trans>
                }
                rows={8}
                spellCheck={false}
                variant="secondary"
                className="font-mono"
              />
            )}
          </form.AppField>

          <OidcSubject value={account.oidcSubject} />

          {saved && (
            <p role="status" className="text-sm">
              <Trans id="admin.user.saved">This account is updated.</Trans>
            </p>
          )}
          <form.SubmitButton>
            <Trans id="admin.user.save">Save changes</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}

/** The subject downstream applications see, which the server alone decides. */
function OidcSubject({ value }: { value: string }) {
  const { t } = useLingui();
  const [copied, setCopied] = useState(false);

  return (
    <div className="flex flex-col gap-1">
      <span className="text-sm text-muted">
        <Trans id="admin.user.oidcSubject">OIDC subject</Trans>
      </span>
      <div className="flex flex-wrap items-center gap-2">
        <span className="wrap-anywhere font-mono text-sm">{value}</span>
        <Button
          size="sm"
          variant="secondary"
          onPress={() => {
            void navigator.clipboard
              .writeText(value)
              .then(() => setCopied(true))
              .catch(() => setCopied(false));
          }}
        >
          {t({ id: "admin.user.oidcSubject.copy", message: "Copy" })}
        </Button>
      </div>
      {copied && (
        <p role="status" className="text-sm">
          <Trans id="admin.user.copied">Copied.</Trans>
        </p>
      )}
      <Description>
        <Trans id="admin.user.oidcSubject.hint">
          Applications identify this account by this value. It never changes.
        </Trans>
      </Description>
    </div>
  );
}

/**
 * Everything about how this account gets in, and the ways to cut it off: one
 * list per kind of sign-in, all on screen at once, so an admin reads the whole
 * picture without opening anything.
 */
function AccessPanel({ accountId }: { accountId: number }) {
  return (
    <div className="flex flex-col gap-4">
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
  const format = useDateFormat();
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
              <Trans key="linked" id="admin.user.identities.detail.linked">
                Linked {format(identity.linkedAt)}
              </Trans>,
            ]}
          />
        ))}
      </ItemList>
    </div>
  );
}

function PasskeysBlock({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const format = useDateFormat();
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
          const lastUsed = format(credential.lastUsedAt);
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
                  Added {format(credential.createdAt)}
                </Trans>,
                lastUsed === null ? (
                  <Trans key="used" id="admin.user.passkeys.never_used">
                    Not used yet
                  </Trans>
                ) : (
                  <Trans key="used" id="admin.user.passkeys.detail.lastUsed">
                    Last used {lastUsed}
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
  const format = useDateFormat();
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
                Started {format(session.issuedAt)}
              </Trans>,
              <Trans key="expires" id="admin.user.sessions.detail.expires">
                Expires {format(session.expiresAt)}
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
          const lastUsed = format(token.lastUsedAt);
          return (
            <ItemListRow
              key={token.id}
              icon={<Ticket size={18} aria-hidden="true" />}
              title={token.name}
              details={[
                <span key="hint" className="font-mono">
                  …{token.tokenHint}
                </span>,
                expires === null ? (
                  <Trans key="expires" id="admin.user.tokens.detail.noExpiry">
                    Never expires
                  </Trans>
                ) : (
                  <Trans key="expires" id="admin.user.tokens.detail.expires">
                    Expires {expires}
                  </Trans>
                ),
                lastUsed === null ? (
                  <Trans key="used" id="admin.user.tokens.never_used">
                    Not used yet
                  </Trans>
                ) : (
                  <Trans key="used" id="admin.user.tokens.detail.lastUsed">
                    Last used {lastUsed}
                  </Trans>
                ),
              ]}
              actions={
                <DangerZone
                  size="sm"
                  label={<Trans id="admin.user.tokens.revoke">Revoke</Trans>}
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

/**
 * The three actions that end or reset an account, one row each with its own
 * confirmation, all on screen at once.
 */
function DangerPanel({ account }: { account: Account }) {
  const { t } = useLingui();
  return (
    <ItemList
      title={<Trans id="admin.user.danger.title">Danger zone</Trans>}
      label={t({ id: "admin.user.danger.title", message: "Danger zone" })}
      empty={null}
    >
      <EnrollmentBlock key="enrollment" accountId={account.id} />
      <DisabledBlock key="disabled" account={account} />
      <DeleteBlock key="delete" account={account} />
    </ItemList>
  );
}

/**
 * Issues a fresh registration link for the account.
 *
 * The link is a credential: anyone holding it can set this account's sign-in
 * method, and the server does not show it again, so it is revealed through
 * `SecretReveal` in a dialog over the page rather than printed on the card.
 */
function EnrollmentBlock({ accountId }: { accountId: number }) {
  const { t, i18n } = useLingui();
  const reissue = useMutation(reissueEnrollmentMutationOptions());
  const [link, setLink] = useState<{ url: string; expiresAt: string } | null>(
    null,
  );

  // A reset link the account is meant to use itself, unlike an invitation that
  // signs its holder straight in, so the wording is the invitation's and the
  // expiry is said on the card rather than inside the reveal.
  const expiry =
    link === null
      ? ""
      : new Intl.DateTimeFormat(i18n.locale, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(link.expiresAt));

  return (
    <ItemListRow
      title={<Trans id="admin.user.enrollment.title">Registration link</Trans>}
      details={[
        <Trans key="note" id="admin.user.enrollment.note">
          Issues a new link this account can use to set up how they sign in. The
          link is shown once, and any earlier link stops working.
        </Trans>,
      ]}
      actions={
        <Button
          size="sm"
          variant="secondary"
          isPending={reissue.isPending}
          onPress={() => {
            reissue.mutate(accountId, {
              onSuccess: (result) => setLink(result),
            });
          }}
        >
          <Trans id="admin.user.enrollment.action">
            Issue a registration link
          </Trans>
        </Button>
      }
    >
      {reissue.error !== null && <BlockError error={reissue.error} onSurface />}
      {link !== null && (
        <p className="text-xs text-muted">
          <Trans id="admin.user.enrollment.expires">
            The link expires on {expiry}.
          </Trans>
        </p>
      )}
      {link !== null && (
        // Open exactly while the link is held here, so it cannot outlive it and
        // only the reveal's own continue control can close it.
        <Modal isOpen onOpenChange={() => {}}>
          <Modal.Backdrop isDismissable={false} isKeyboardDismissDisabled>
            <Modal.Container placement="center" size="lg" scroll="inside">
              <Modal.Dialog>
                <Modal.Header>
                  <Modal.Heading>{t(invitationLinkCopy.title)}</Modal.Heading>
                </Modal.Header>
                <SecretReveal
                  text={link.url}
                  filename="prohibitorum-registration-link.txt"
                  copy={invitationLinkCopy}
                  onContinue={async () => setLink(null)}
                  inDialog
                />
              </Modal.Dialog>
            </Modal.Container>
          </Modal.Backdrop>
        </Modal>
      )}
    </ItemListRow>
  );
}

function DisabledBlock({ account }: { account: Account }) {
  const queryClient = useQueryClient();
  const setDisabled = useMutation(
    setAccountDisabledMutationOptions(queryClient),
  );

  return (
    <ItemListRow
      title={<Trans id="admin.user.state.title">Enabled or disabled</Trans>}
      details={[
        account.disabled ? (
          <Trans key="note" id="admin.user.state.disabled.note">
            This account cannot sign in. Enabling it restores access with the
            sign-in methods it already had.
          </Trans>
        ) : (
          <Trans key="note" id="admin.user.state.enabled.note">
            Disabling stops this account signing in and ends nothing else. An
            admin has to be made a user first.
          </Trans>
        ),
      ]}
      actions={
        <DangerZone
          size="sm"
          label={
            account.disabled ? (
              <Trans id="admin.user.state.enable">Enable this account</Trans>
            ) : (
              <Trans id="admin.user.state.disable">Disable this account</Trans>
            )
          }
          title={
            account.disabled ? (
              <Trans id="admin.user.state.enable.title">
                Enable this account?
              </Trans>
            ) : (
              <Trans id="admin.user.state.disable.title">
                Disable this account?
              </Trans>
            )
          }
          body={
            <p>
              {account.disabled ? (
                <Trans id="admin.user.state.enable.body">
                  They will be able to sign in again straight away.
                </Trans>
              ) : (
                <Trans id="admin.user.state.disable.body">
                  They will not be able to sign in. Sessions they already have
                  stay active until you end them.
                </Trans>
              )}
            </p>
          }
          confirmLabel={
            account.disabled ? (
              <Trans id="admin.user.state.enable.action">Enable</Trans>
            ) : (
              <Trans id="admin.user.state.disable.action">Disable</Trans>
            )
          }
          isPending={setDisabled.isPending}
          onConfirm={() =>
            setDisabled.mutate({ id: account.id, disabled: !account.disabled })
          }
        />
      }
    >
      {setDisabled.error !== null && (
        <BlockError error={setDisabled.error} onSurface />
      )}
    </ItemListRow>
  );
}

function DeleteBlock({ account }: { account: Account }) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const remove = useMutation(deleteAccountMutationOptions(queryClient));

  return (
    <ItemListRow
      title={<Trans id="admin.user.delete.title">Delete this account</Trans>}
      details={[
        <Trans key="note" id="admin.user.delete.note">
          Removes the account and everything it owns: passkeys, sessions, access
          tokens and connected identities. This cannot be undone.
        </Trans>,
      ]}
      actions={
        <DangerZone
          size="sm"
          label={
            <Trans id="admin.user.delete.action">Delete this account</Trans>
          }
          title={
            <Trans id="admin.user.delete.confirm.title">
              Delete this account?
            </Trans>
          }
          body={
            <p>
              <Trans id="admin.user.delete.confirm.body">
                The account, its passkeys, its sessions, its access tokens and
                its connected identities are removed immediately. This cannot be
                undone.
              </Trans>
            </p>
          }
          confirmLabel={
            <Trans id="admin.user.delete.confirm.action">Delete account</Trans>
          }
          isPending={remove.isPending}
          onConfirm={() =>
            remove.mutate(account.id, {
              onSuccess: async () => {
                await navigate({
                  to: "/admin/users",
                  search: {
                    q: "",
                    provider: "",
                    field: "",
                    value: "",
                    match: "",
                    role: "",
                    state: "",
                  },
                });
              },
            })
          }
        />
      }
    >
      {remove.error !== null && <BlockError error={remove.error} onSurface />}
    </ItemListRow>
  );
}
