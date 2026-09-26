import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateForwardAuthAppMutationOptions } from "@/api/mutations";
import { forwardAuthAppUpdateBody } from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { CopyValue } from "@/components/custom/CopyValue";
import { EntityIconCard } from "@/components/custom/EntityIconCard";
import { RowsField } from "@/components/custom/RowsField";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  hostProblem,
  scopeListProblem,
} from "@/pages/admin/forward-auth-apps/forward-auth-validation";
import { ScopeRows } from "@/pages/admin/forward-auth-apps/ScopeRows";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

const nameRequired = msg({
  id: "admin.forward-auth-apps.field.name.required",
  message: "Enter a name.",
});

/**
 * What the application is and which host it protects.
 *
 * The Client ID is shown but never editable: the server takes it only on
 * create, and the operator's Traefik router is already configured against it.
 * `CopyValue` is how it comes back out, which is the one thing a reader opens
 * this card to do.
 *
 * The hostname is editable and is the value the snippet in the proxy section is
 * generated from, so a reader who changes it here sees the configuration below
 * change with it. The server does not check its shape at all, which is why the
 * console does.
 *
 * The whole form submits through `forwardAuthAppUpdateBody`. `PUT` replaces the
 * record rather than patching it, so every field this card does not draw is
 * carried from the saved view — the body module is where that merge lives, and
 * a card that built it itself is how a field ends up cleared by a save from
 * somewhere else.
 */
export function ForwardAuthGeneralSection({ app }: { app: ForwardAuthApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateForwardAuthAppMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: {
      displayName: app.displayName,
      host: app.forwardAuthHost,
      scopes: (app.scopes ?? []).map((scope) => ({
        name: scope.name,
        description: scope.description ?? "",
      })),
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
        await update.mutateAsync({
          clientId: app.clientId,
          body: forwardAuthAppUpdateBody(app, {
            displayName: value.displayName.trim(),
            host,
            // An empty description is omitted rather than sent empty: the
            // server reports one back only when it is non-empty, so sending
            // `""` would make the next read disagree with what was saved.
            scopes: value.scopes.map((scope) => ({
              name: scope.name,
              ...(scope.description.trim() === ""
                ? {}
                : { description: scope.description.trim() }),
            })),
          }),
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          // A hostname another application already claims arrives as the same
          // duplicate code, and the host is the field it is about.
          codes: { oidc_client_already_exists: "host", bad_request: "host" },
          locations: {},
        });
      }
    },
  });

  return (
    <Section
      title={<Trans id="admin.forward-auth-apps.general">General</Trans>}
    >
      <ConsoleCard>
        <form.AppForm>
          <form.Form
            label={t({
              id: "admin.forward-auth-apps.general",
              message: "General",
            })}
          >
            <form.FormError />

            <CopyValue
              value={app.clientId}
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
            />

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
                      The host the application is served on, as Traefik matches
                      it — no scheme and no port.
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
                      grants a subset of these, and the gateway sends the
                      granted names on as Remote-Scopes.
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

            <form.SubmitButton>
              <Trans id="admin.forward-auth-apps.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>
    </Section>
  );
}

/**
 * The application's icon, in its own section beside the general one.
 *
 * Separate rather than folded into the general card, because it is drawn for
 * every entity on the console from one component and the identity providers
 * already stack it this way: the general card is what the application *is*, the
 * icon is how it looks in every list it appears in.
 */
export function ForwardAuthIconSection({ app }: { app: ForwardAuthApp }) {
  return (
    <Section title={<Trans id="admin.forward-auth-apps.icon">Icon</Trans>}>
      <EntityIconCard
        target={{ kind: "forward_auth", appId: app.clientId }}
        iconUrl={app.iconUrl}
        name={app.displayName || app.clientId}
      />
    </Section>
  );
}
