import {
  AlertDialog,
  Button,
  Card,
  Checkbox,
  Description,
  Spinner,
} from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  type CreateTokenInput,
  createTokenMutationOptions,
  revokeTokenMutationOptions,
} from "@/api/mutations";
import { forwardAuthAppsQueryOptions, tokensQueryOptions } from "@/api/queries";
import { runWithSudo } from "@/api/sudo";
import { sudoReason } from "@/api/sudo-reasons";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { SecretReveal } from "@/components/custom/SecretReveal";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { accessTokenCopy } from "@/components/custom/secret-reveal-copy";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type Token = components["schemas"]["PersonalAccessTokenView"];

const nameRequired = msg({
  id: "security.tokens.name.required",
  message: "Give the token a name so you can recognise it later.",
});
const chooseApp = msg({
  id: "security.tokens.apps.required",
  message: "Choose at least one application, or allow every application.",
});

/**
 * Personal access tokens.
 *
 * A token's scopes have to be chosen from the vocabulary each forward-auth
 * application publishes, so the form is built from `GET /me/forward-auth-apps`
 * rather than letting a caller type a scope. "Every application" and the
 * per-application list are mutually exclusive: the server rejects a request that
 * sets both, which is why the grants are dropped from the body in that case.
 */
export function TokensPanel() {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const tokens = useQuery(tokensQueryOptions());
  const apps = useQuery(forwardAuthAppsQueryOptions());
  const [plaintext, setPlaintext] = useState<string | null>(null);
  const [target, setTarget] = useState<Token | null>(null);
  const [error, setError] = useState<unknown>(null);
  const revoke = useMutation(revokeTokenMutationOptions(queryClient));

  const format = (value?: string) =>
    value === undefined
      ? null
      : new Intl.DateTimeFormat(i18n.locale, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(value));

  const scopeSummary = (token: Token) => {
    if (token.allApps) {
      return <Trans id="security.tokens.all_apps">Every application</Trans>;
    }
    const entries = Object.entries(token.appGrants ?? {});
    if (entries.length === 0) {
      return (
        <span className="text-muted">
          <Trans id="security.tokens.no_apps">No applications</Trans>
        </span>
      );
    }
    const nameOf = (clientId: string) =>
      (apps.data ?? []).find((app) => app.clientId === clientId)?.displayName ??
      clientId;
    return (
      <span className="wrap-anywhere">
        {entries
          .map(
            ([clientId, scopes]) =>
              `${nameOf(clientId)}: ${(scopes ?? []).join(", ") || "—"}`,
          )
          .join("; ")}
      </span>
    );
  };

  const columns: readonly TableColumn<Token>[] = [
    {
      id: "name",
      header: <Trans id="security.tokens.column.name">Name</Trans>,
      cell: (token) => (
        <span className="wrap-anywhere font-medium">{token.name}</span>
      ),
    },
    {
      id: "hint",
      header: <Trans id="security.tokens.column.hint">Token</Trans>,
      cell: (token) => (
        <span className="font-mono text-xs">…{token.tokenHint}</span>
      ),
    },
    {
      id: "createdAt",
      header: <Trans id="security.tokens.column.created">Created</Trans>,
      cell: (token) => format(token.createdAt),
    },
    {
      id: "expiresAt",
      header: <Trans id="security.tokens.column.expires">Expires</Trans>,
      cell: (token) =>
        format(token.expiresAt) ?? (
          <span className="text-muted">
            <Trans id="security.tokens.never_expires">Never</Trans>
          </span>
        ),
    },
    {
      id: "lastUsedAt",
      header: <Trans id="security.tokens.column.lastUsed">Last used</Trans>,
      cell: (token) =>
        format(token.lastUsedAt) ?? (
          <span className="text-muted">
            <Trans id="security.tokens.never_used">Not used yet</Trans>
          </span>
        ),
    },
    {
      id: "scopes",
      header: <Trans id="security.tokens.column.scopes">Allowed</Trans>,
      cell: scopeSummary,
    },
    {
      id: "actions",
      header: <Trans id="security.column.actions">Actions</Trans>,
      cell: (token) => (
        <Button
          size="sm"
          variant="danger-soft"
          onPress={() => setTarget(token)}
        >
          <Trans id="security.tokens.revoke">Revoke</Trans>
        </Button>
      ),
      align: "end",
    },
  ];

  if (plaintext !== null) {
    return (
      <SecretReveal
        text={plaintext}
        filename="prohibitorum-access-token.txt"
        copy={accessTokenCopy}
        onContinue={async () => {
          setPlaintext(null);
        }}
      />
    );
  }

  return (
    <>
      {error !== null && (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>{t(describeError(error))}</SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      <DataTable
        label={t({ id: "security.tokens.table", message: "Access tokens" })}
        columns={columns}
        rows={tokens.data ?? []}
        rowId={(token) => token.id}
        loading={tokens.isPending}
        empty={
          <Trans id="security.tokens.empty">
            You have no access tokens yet.
          </Trans>
        }
      />

      <CreateTokenCard
        onCreated={setPlaintext}
        onError={setError}
        apps={apps.data ?? []}
      />

      <AlertDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
      >
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>
                  <Trans id="security.tokens.revoke.title">
                    Revoke this token?
                  </Trans>
                </AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>
                <p>
                  <Trans id="security.tokens.revoke.body">
                    Anything using this token stops working immediately. This
                    cannot be undone.
                  </Trans>
                </p>
              </AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  onPress={() => setTarget(null)}
                  isDisabled={revoke.isPending}
                >
                  <Trans id="security.cancel">Cancel</Trans>
                </Button>
                <Button
                  variant="danger"
                  isPending={revoke.isPending}
                  onPress={() => {
                    if (!target) return;
                    revoke.mutate(target.id, {
                      onSuccess: () => setTarget(null),
                      onError: (failure) => {
                        setError(failure);
                        setTarget(null);
                      },
                    });
                  }}
                >
                  <Trans id="security.tokens.revoke.action">Revoke</Trans>
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
 * Builds the create request, keeping `allApps` and `appGrants` mutually
 * exclusive: the server rejects a body that sets grants while also allowing
 * every application, so the grants are dropped rather than sent and ignored.
 * Days of zero or less mean "no expiry", which the server takes as omitted.
 */
export function buildTokenRequest(input: {
  name: string;
  allApps: boolean;
  grants: Record<string, string[]>;
  expiresInDays: string;
}): CreateTokenInput {
  const days = Number(input.expiresInDays);
  return {
    name: input.name,
    allApps: input.allApps,
    appGrants: input.allApps ? {} : input.grants,
    ...(Number.isInteger(days) && days > 0 ? { expiresInDays: days } : {}),
  };
}

/** A scope is only offerable if the application publishes it. */
export function isOfferedScope(
  apps: readonly ForwardAuthApp[],
  clientId: string,
  scope: string,
): boolean {
  const app = apps.find((candidate) => candidate.clientId === clientId);
  return app?.scopes?.some((entry) => entry.name === scope) ?? false;
}

type ForwardAuthApp = components["schemas"]["MyForwardAuthApp"];

function CreateTokenCard({
  apps,
  onCreated,
  onError,
}: {
  apps: ForwardAuthApp[];
  onCreated: (token: string) => void;
  onError: (error: unknown) => void;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const [open, setOpen] = useState(false);
  const [allApps, setAllApps] = useState(true);
  const [expiresInDays, setExpiresInDays] = useState("0");
  const [grants, setGrants] = useState<Record<string, string[]>>({});

  const create = useMutation(createTokenMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: { name: "" },
    onSubmit: async ({ value }) => {
      const name = value.name.trim();
      if (name === "") {
        form.setFieldMeta("name", (meta) => ({
          ...meta,
          errorMap: { ...meta.errorMap, onSubmit: nameRequired },
        }));
        return;
      }
      if (!allApps && Object.keys(grants).length === 0) {
        form.setErrorMap({ onSubmit: { form: chooseApp, fields: {} } });
        return;
      }
      const body = buildTokenRequest({ name, allApps, grants, expiresInDays });
      try {
        const result = await runWithSudo(
          () => create.mutateAsync(body),
          sudoReason.createToken,
        );
        const created = result as { token?: unknown };
        if (typeof created?.token !== "string") return;
        setOpen(false);
        setGrants({});
        setAllApps(true);
        form.reset();
        onCreated(created.token);
      } catch (failure) {
        if (isCancellation(failure)) return;
        applyServerError(form, failure, {
          locations: { name: "name" },
          codes: {},
        });
        onError(failure);
      }
    },
  });

  return (
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>
          <Trans id="security.tokens.create.title">New access token</Trans>
        </Card.Title>
      </Card.Header>
      <Card.Content className="flex flex-col gap-4">
        <Description>
          <Trans id="security.tokens.create.note">
            The token itself is shown once, right after you create it. Copy it
            then; the list will only ever show the last few characters.
          </Trans>
        </Description>
        {!open ? (
          <div>
            <Button onPress={() => setOpen(true)}>
              <Trans id="security.tokens.create.open">Create a token</Trans>
            </Button>
          </div>
        ) : (
          <form.AppForm>
            <form.Form
              label={t({
                id: "security.tokens.create.form",
                message: "New access token",
              })}
            >
              <form.FormError />
              <form.AppField
                name="name"
                validators={{
                  onChange: ({ value }) =>
                    value.trim() === "" ? nameRequired : undefined,
                }}
              >
                {(field) => (
                  <field.FormField
                    label={<Trans id="security.tokens.name">Name</Trans>}
                    description={
                      <Trans id="security.tokens.name.hint">
                        What will use this token, so you know what to revoke
                        later.
                      </Trans>
                    }
                    autoComplete="off"
                  />
                )}
              </form.AppField>

              <div className="flex flex-col gap-2">
                <span className="text-sm font-medium">
                  <Trans id="security.tokens.expiry">Expires after</Trans>
                </span>
                <select
                  className="h-9 rounded-field border border-separator bg-surface px-3 text-sm"
                  value={expiresInDays}
                  onChange={(event) => setExpiresInDays(event.target.value)}
                  aria-label={t({
                    id: "security.tokens.expiry.label",
                    message: "Expires after",
                  })}
                >
                  <option value="0">
                    {t({
                      id: "security.tokens.expiry.never",
                      message: "Never expires",
                    })}
                  </option>
                  <option value="30">
                    {t({
                      id: "security.tokens.expiry.30",
                      message: "30 days",
                    })}
                  </option>
                  <option value="90">
                    {t({
                      id: "security.tokens.expiry.90",
                      message: "90 days",
                    })}
                  </option>
                  <option value="365">
                    {t({
                      id: "security.tokens.expiry.365",
                      message: "1 year",
                    })}
                  </option>
                </select>
              </div>

              <div className="flex flex-col gap-3">
                <span className="text-sm font-medium">
                  <Trans id="security.tokens.scope">What it may access</Trans>
                </span>
                <Checkbox
                  isSelected={allApps}
                  onChange={(selected) => setAllApps(selected === true)}
                >
                  <Checkbox.Content>
                    <Checkbox.Control>
                      <Checkbox.Indicator />
                    </Checkbox.Control>
                    <Trans id="security.tokens.every_app">
                      Every application you can use
                    </Trans>
                  </Checkbox.Content>
                </Checkbox>
                {!allApps && (
                  <div className="flex flex-col gap-4 border-separator border-s ps-4">
                    {apps.length === 0 ? (
                      <p className="text-sm text-muted">
                        <Trans id="security.tokens.no_forward_auth">
                          You have no applications available to grant.
                        </Trans>
                      </p>
                    ) : (
                      apps.map((app) => (
                        <div key={app.clientId} className="flex flex-col gap-2">
                          <span className="text-sm font-medium wrap-anywhere">
                            {app.displayName}
                          </span>
                          {(app.scopes ?? []).length === 0 ? (
                            <span className="text-sm text-muted">
                              <Trans id="security.tokens.app_no_scopes">
                                This application defines no scopes.
                              </Trans>
                            </span>
                          ) : (
                            (app.scopes ?? []).map((scope) => {
                              const selected =
                                grants[app.clientId]?.includes(scope.name) ??
                                false;
                              return (
                                <Checkbox
                                  key={`${app.clientId}:${scope.name}`}
                                  isSelected={selected}
                                  onChange={(next) => {
                                    setGrants((current) => {
                                      const list = new Set(
                                        current[app.clientId] ?? [],
                                      );
                                      if (next === true) list.add(scope.name);
                                      else list.delete(scope.name);
                                      const updated = { ...current };
                                      if (list.size === 0)
                                        delete updated[app.clientId];
                                      else updated[app.clientId] = [...list];
                                      return updated;
                                    });
                                  }}
                                >
                                  <Checkbox.Content>
                                    <Checkbox.Control>
                                      <Checkbox.Indicator />
                                    </Checkbox.Control>
                                    <span className="flex flex-col gap-0.5">
                                      <span className="wrap-anywhere font-mono text-xs">
                                        {scope.name}
                                      </span>
                                      {scope.description && (
                                        <span className="text-sm text-muted">
                                          {scope.description}
                                        </span>
                                      )}
                                    </span>
                                  </Checkbox.Content>
                                </Checkbox>
                              );
                            })
                          )}
                        </div>
                      ))
                    )}
                  </div>
                )}
              </div>

              <div className="flex flex-wrap gap-2">
                <form.SubmitButton>
                  <Trans id="security.tokens.create.action">Create token</Trans>
                </form.SubmitButton>
                <Button
                  variant="secondary"
                  isDisabled={create.isPending}
                  onPress={() => setOpen(false)}
                >
                  <Trans id="security.cancel">Cancel</Trans>
                </Button>
                {create.isPending && <Spinner size="sm" />}
              </div>
            </form.Form>
          </form.AppForm>
        )}
      </Card.Content>
    </Card>
  );
}
