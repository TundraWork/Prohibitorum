import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateOidcAppMutationOptions } from "@/api/mutations";
import type { UpdateOidcAppRequest } from "@/api/raw-admin-paths";
import { oidcAppUpdateBody } from "@/api/update-bodies";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { CopyValue } from "@/components/custom/CopyValue";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { OidcScopesField } from "@/pages/admin/oidc-applications/OidcScopesField";
import {
  removedScopes,
  scopeFormValue,
  uriListProblem,
} from "@/pages/admin/oidc-applications/oidc-validation";
import {
  UriListField,
  uriRowProblem,
} from "@/pages/admin/oidc-applications/UriListField";

type OidcApp = components["schemas"]["OIDCApplicationView"];

/**
 * The values a client's own configuration has to match: its Client ID, the
 * addresses it may return to, and the scopes it may ask for.
 *
 * First on the page because they are what a reader comes to copy or check
 * against the client. The Client ID is read-only — the server takes it only on
 * create — so `CopyValue` is how it comes back out.
 *
 * The form submits through `oidcAppUpdateBody`. `PUT` replaces the record, so
 * the fields drawn by the other sections go out as they are stored, and an
 * unsaved edit there never leaves with a save here.
 *
 * Removing a saved scope is confirmed when the form is saved: a client that
 * still requests it is refused at `/authorize` from then on, while refresh
 * tokens already issued keep it until they expire or are revoked. The save
 * button turns to its warning tone as soon as a saved scope is missing, and the
 * dialog names the scopes. Adding a scope or editing an address saves straight
 * away.
 */
export function ClientConfigSection({ app }: { app: OidcApp }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateOidcAppMutationOptions(queryClient));
  // The request the dialog is confirming, checked and ready to send, with the
  // scopes it removes. It outlives the dialog's closing so the text does not
  // change while the dialog fades out.
  const [pending, setPending] = useState<{
    body: UpdateOidcAppRequest;
    removed: string[];
  } | null>(null);
  const [confirming, setConfirming] = useState(false);

  const savedScopes = app.allowedScopes ?? [];
  const savedRedirects = app.redirectUris ?? [];

  const form = useAppForm({
    defaultValues: {
      // Never empty: a client signs in through at least one of these, so the
      // list starts with a row to fill and its last row cannot be removed.
      redirectUris: savedRedirects.length > 0 ? savedRedirects : [""],
      postLogoutRedirectUris: app.postLogoutRedirectUris ?? [],
      allowedScopes: scopeFormValue(savedScopes),
    },
    onSubmit: async ({ value }) => {
      for (const name of ["redirectUris", "postLogoutRedirectUris"] as const) {
        const problem = uriListProblem(value[name]);
        if (problem !== undefined) {
          form.setFieldMeta(name, (meta) => ({
            ...meta,
            errorMap: {
              ...meta.errorMap,
              onSubmit: [uriRowProblem(problem)],
            },
          }));
          return;
        }
      }

      const body = oidcAppUpdateBody(app, {
        redirectUris: value.redirectUris,
        postLogoutRedirectUris: value.postLogoutRedirectUris,
        allowedScopes: value.allowedScopes,
      });
      const removed = removedScopes(savedScopes, value.allowedScopes);
      if (removed.length > 0) {
        setPending({ body, removed });
        setConfirming(true);
        return;
      }
      await save(body);
    },
  });

  async function save(body: UpdateOidcAppRequest) {
    try {
      await update.mutateAsync({ clientId: app.clientId, body });
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, {
        codes: { bad_request: "allowedScopes" },
        locations: {},
      });
    }
  }

  const removesScopes = useStore(
    form.store,
    (state) =>
      removedScopes(savedScopes, state.values.allowedScopes).length > 0,
  );

  const title = t({
    id: "admin.oidc-apps.client",
    message: "Client configuration",
  });
  const removedNames = pending?.removed ?? [];
  const removedCount = pending?.removed.length ?? 0;

  return (
    <Section title={title}>
      <ConsoleCard>
        <form.AppForm>
          <form.Form label={title}>
            <form.FormError />

            <CopyValue
              value={app.clientId}
              label={
                <Trans id="admin.oidc-apps.field.client-id">Client ID</Trans>
              }
            />

            <form.AppField name="redirectUris">
              {() => (
                <UriListField
                  label={t({
                    id: "admin.oidc-apps.field.redirects",
                    message: "Redirect URIs",
                  })}
                  addLabel={
                    <Trans id="admin.oidc-apps.uri.add">Add address</Trans>
                  }
                  placeholder="https://app.example.com/callback"
                  minRows={1}
                  minRowsReason={
                    <Trans id="admin.oidc-apps.field.redirect.required">
                      A client needs at least one redirect URI.
                    </Trans>
                  }
                />
              )}
            </form.AppField>

            <form.AppField name="postLogoutRedirectUris">
              {() => (
                <UriListField
                  label={t({
                    id: "admin.oidc-apps.field.post-logout",
                    message: "Post-logout redirect URIs",
                  })}
                  description={
                    <Trans id="admin.oidc-apps.field.post-logout.description">
                      Where the client may return after signing out.
                    </Trans>
                  }
                  addLabel={
                    <Trans id="admin.oidc-apps.uri.add">Add address</Trans>
                  }
                  placeholder="https://app.example.com/signed-out"
                />
              )}
            </form.AppField>

            <form.AppField name="allowedScopes">
              {() => <OidcScopesField />}
            </form.AppField>

            <form.SubmitButton tone={removesScopes ? "warning" : "default"}>
              <Trans id="admin.oidc-apps.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>

      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={(open) => {
          if (!open && !update.isPending) setConfirming(false);
        }}
        status="warning"
        title={
          <Plural
            id="admin.oidc-apps.scopes.remove.title"
            value={removedCount}
            one="Remove # scope?"
            other="Remove # scopes?"
          />
        }
        body={
          <p>
            <Trans id="admin.oidc-apps.scopes.remove.body">
              Clients that still request{" "}
              <ScopeNames names={removedNames} locale={i18n.locale} /> can no
              longer sign in until they stop requesting{" "}
              <Plural value={removedCount} one="it" other="them" />. Refresh
              tokens already issued keep{" "}
              <Plural value={removedCount} one="it" other="them" /> until they
              expire or are revoked.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.oidc-apps.scopes.remove.action">
            Remove and save
          </Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (pending === null) return;
          // Closed once the write settles either way: a refusal is reported
          // on the form, where the reader can act on it.
          void save(pending.body).finally(() => setConfirming(false));
        }}
      />
    </Section>
  );
}

/**
 * Scope names joined as the reader's language joins a list, with each name in
 * monospace — it is the string a client sends — and the words between them in
 * the body's own face.
 */
function ScopeNames({
  names,
  locale,
}: {
  names: readonly string[];
  locale: string;
}) {
  const parts = new Intl.ListFormat(locale, {
    type: "conjunction",
  }).formatToParts(names);
  return (
    <>
      {parts.map((part, at) =>
        part.type === "element" ? (
          // biome-ignore lint/suspicious/noArrayIndexKey: parts have no identity beyond their position
          <span key={at} className="font-mono">
            {part.value}
          </span>
        ) : (
          part.value
        ),
      )}
    </>
  );
}
