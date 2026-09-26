import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { isCancellation } from "@/api/errors";
import { createForwardAuthAppMutationOptions } from "@/api/mutations";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { RowsField } from "@/components/custom/RowsField";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  hostProblem,
  scopeListProblem,
} from "@/pages/admin/forward-auth-apps/forward-auth-validation";
import { ScopeRows } from "@/pages/admin/forward-auth-apps/ScopeRows";

/**
 * The Client ID rule, identical to the OIDC application's.
 *
 * The server accepts any non-empty string; this refuses one that would not
 * survive the trip. The identifier becomes a path segment in the access and
 * manager endpoints and part of a URL the operator is configured with, so a `?`
 * or a space is a problem to discover later. Leading alphanumeric keeps it out
 * of the reach of a path traversal and of a client that treats a leading dot as
 * a hidden file.
 */
const clientIdPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

const clientIdRequired = msg({
  id: "admin.forward-auth-apps.new.client-id.required",
  message: "Enter a Client ID.",
});

const clientIdInvalid = msg({
  id: "admin.forward-auth-apps.new.client-id.invalid",
  message:
    "Use letters, digits, dots, underscores and hyphens, starting with a letter or a digit, up to 128 characters.",
});

const nameRequired = msg({
  id: "admin.forward-auth-apps.new.name.required",
  message: "Enter a name.",
});

/**
 * Registering one forward-auth application.
 *
 * Only what the server cannot default is asked for. The scopes, the identity
 * projection, the icon and the access policy all have a working default — the
 * record's own — and are set on the detail page, where the application as it
 * currently stands is beside the field. What cannot be defaulted is what the
 * application *is*: its identifier, the host it protects, and the vocabulary
 * its tokens may ask for, since the server takes the Client ID and the host
 * only at creation.
 *
 * A scope vocabulary is offered here rather than left to the detail page
 * because it is part of the application's contract with the upstream service:
 * a token's grants are validated against it, so a deployment that forgot to
 * declare one has a service that refuses every token with no obvious reason.
 */
export function AdminForwardAuthAppNew() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const create = useMutation(createForwardAuthAppMutationOptions());

  const form = useAppForm({
    defaultValues: {
      displayName: "",
      clientId: "",
      host: "",
      scopes: [] as { name: string; description: string }[],
      accessRestricted: false,
    },
    onSubmit: async ({ value }) => {
      const host = value.host.trim();
      const hostIssue = hostProblem(host);
      if (hostIssue !== undefined) {
        form.setFieldMeta("host", (meta) => ({ ...meta, errors: [hostIssue] }));
        return;
      }
      const scopeIssue = scopeListProblem(value.scopes);
      if (scopeIssue !== undefined) {
        form.setFieldMeta("scopes", (meta) => ({
          ...meta,
          errors: [scopeIssue],
        }));
        return;
      }

      try {
        const created = await create.mutateAsync({
          clientId: value.clientId.trim(),
          host,
          displayName: value.displayName.trim() || undefined,
          // An empty description is omitted rather than sent empty: the server
          // reports it back only when it is non-empty, so sending `""` would
          // make the round trip disagree with itself.
          scopes: value.scopes.map((scope) => ({
            name: scope.name,
            ...(scope.description.trim() === ""
              ? {}
              : { description: scope.description.trim() }),
          })),
          accessRestricted: value.accessRestricted,
        });
        await navigate({
          to: "/admin/forward-auth-apps/$clientId",
          params: { clientId: created.clientId },
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          // A duplicate Client ID and a duplicate hostname are the same code
          // from the server; the Client ID is the field that names the request,
          // so the message lands there.
          codes: { oidc_client_already_exists: "clientId" },
          locations: {},
        });
      }
    },
  });

  return (
    <ConsoleCard>
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.forward-auth-apps.new.form",
            message: "New forward-auth application",
          })}
        >
          <form.FormError />

          <form.AppField
            name="displayName"
            validators={{
              onSubmit: ({ value }) =>
                value.trim() === "" ? nameRequired : undefined,
            }}
          >
            {(field) => (
              <field.FormField
                label={
                  <Trans id="admin.forward-auth-apps.field.name">Name</Trans>
                }
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField
            name="clientId"
            validators={{
              onSubmit: ({ value }) => {
                const clientId = value.trim();
                if (clientId === "") return clientIdRequired;
                return clientIdPattern.test(clientId)
                  ? undefined
                  : clientIdInvalid;
              },
            }}
          >
            {(field) => (
              <field.FormField
                label={
                  <Trans id="admin.forward-auth-apps.field.client-id">
                    Client ID
                  </Trans>
                }
                description={
                  <Trans id="admin.forward-auth-apps.field.client-id.hint">
                    The identifier every request to this application carries. It
                    cannot change later.
                  </Trans>
                }
                autoComplete="off"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField
            name="host"
            validators={{
              onSubmit: ({ value }) => hostProblem(value.trim()),
            }}
          >
            {(field) => (
              <field.FormField
                label={
                  <Trans id="admin.forward-auth-apps.field.host">
                    Hostname
                  </Trans>
                }
                description={
                  <Trans id="admin.forward-auth-apps.field.host.hint">
                    The host the application is served on, as Traefik matches it
                    — no scheme and no port.
                  </Trans>
                }
                autoComplete="off"
                spellCheck={false}
                placeholder="app.example.com"
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField
            name="scopes"
            validators={{
              onSubmit: ({ value }) => scopeListProblem(value),
            }}
          >
            {() => (
              <RowsField
                label={
                  <Trans id="admin.forward-auth-apps.field.scopes">
                    Scope vocabulary
                  </Trans>
                }
                description={
                  <Trans id="admin.forward-auth-apps.field.scopes.hint">
                    The application's own scope names. A personal access token
                    grants a subset of these, and the gateway sends the granted
                    names on as Remote-Scopes.
                  </Trans>
                }
                renderRow={(_row, index, error) => (
                  <ScopeRows index={index} error={error} />
                )}
                emptyRow={() => ({ name: "", description: "" })}
                addLabel={
                  <Trans id="admin.forward-auth-apps.field.scopes.add">
                    Add a scope
                  </Trans>
                }
              />
            )}
          </form.AppField>

          <form.AppField name="accessRestricted">
            {(field) => (
              <field.SwitchField
                label={
                  <Trans id="admin.forward-auth-apps.field.restricted">
                    Restrict access
                  </Trans>
                }
                description={
                  <Trans id="admin.forward-auth-apps.field.restricted.hint">
                    Nobody can use the application until you select a user
                    group.
                  </Trans>
                }
              />
            )}
          </form.AppField>

          <form.SubmitButton>
            <Trans id="admin.forward-auth-apps.new.submit">
              Create application
            </Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}
