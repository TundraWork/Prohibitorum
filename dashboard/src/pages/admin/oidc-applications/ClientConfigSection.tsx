import { Plural, Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LogInIcon } from "lucide-react";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateOidcAppMutationOptions } from "@/api/mutations";
import type { UpdateOidcAppRequest } from "@/api/raw-admin-paths";
import { oidcAppUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { Section } from "@/components/custom/Section";
import { UriList, uriRowProblem } from "@/components/custom/UriList";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { OidcScopesField } from "@/pages/admin/oidc-applications/OidcScopesField";
import {
  removedScopes,
  scopeFormValue,
  uriListProblem,
} from "@/pages/admin/oidc-applications/oidc-validation";
import {
  ConsentSwitch,
  PkceSwitch,
  RedirectUriList,
} from "@/pages/admin/oidc-applications/SignInFields";

type OidcApp = components["schemas"]["OIDCApplicationView"];

/** The form's values for a stored record, also the baseline after a save. */
function signInValues(app: OidcApp) {
  return {
    // Never empty: a client signs in through at least one of these, so the
    // list starts with a row to fill and its last row cannot be removed.
    redirectUris:
      (app.redirectUris ?? []).length > 0 ? (app.redirectUris ?? []) : [""],
    postLogoutRedirectUris: app.postLogoutRedirectUris ?? [],
    allowedScopes: scopeFormValue(app.allowedScopes ?? []),
    // A public client must keep PKCE on, and the switch is locked.
    requirePkce: app.clientAuthMethod === "none" ? true : app.requirePkce,
    requireConsent: app.requireConsent,
  };
}

/**
 * What a client needs to sign people in: the addresses it may return to, the
 * scopes it may ask for, and what the instance asks of it and of the account
 * before it hands over a code — PKCE, and the account's consent.
 *
 * First on the page because the addresses and scopes are what a reader comes to
 * copy or check against the client. The section sits inside its form so the
 * footer's primary button is the form's submit; the footer appears only while
 * something differs from what is saved.
 *
 * The form submits through `oidcAppUpdateBody`. `PUT` replaces the record, so
 * the fields drawn by the other sections go out as they are stored, and an
 * unsaved edit there never leaves with a save here.
 *
 * Removing a saved scope is confirmed when the form is saved: a client that
 * still requests it is refused at `/authorize` from then on, while refresh
 * tokens already issued keep it until they expire or are revoked. The footer
 * says so as soon as a saved scope is cleared, and the dialog names the scopes.
 * Adding a scope or editing an address saves straight away. A failed save is
 * shown on the section alone.
 */
export function ClientConfigSection({ app }: { app: OidcApp }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateOidcAppMutationOptions(queryClient), true),
  );
  // The request the dialog is confirming, checked and ready to send, with the
  // scopes it removes. It outlives the dialog's closing so the text does not
  // change while the dialog fades out.
  const [pending, setPending] = useState<{
    body: UpdateOidcAppRequest;
    removed: string[];
  } | null>(null);
  const [confirming, setConfirming] = useState(false);

  const savedScopes = app.allowedScopes ?? [];
  const isPublic = app.clientAuthMethod === "none";

  const form = useAppForm({
    defaultValues: signInValues(app),
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
        requirePkce: isPublic ? true : value.requirePkce,
        requireConsent: value.requireConsent,
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
      const saved = await update.mutateAsync({ clientId: app.clientId, body });
      // The stored record is the new baseline, so the section reads as saved.
      form.reset(signInValues(saved));
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, {
        codes: { bad_request: "allowedScopes" },
        locations: {},
      });
    }
  }

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const removing = useStore(form.store, (state) =>
    removedScopes(savedScopes, state.values.allowedScopes),
  );

  const title = t({ id: "admin.oidc-apps.sign-in.title", message: "Sign-in" });
  const removedNames = pending?.removed ?? [];
  const removedCount = pending?.removed.length ?? 0;

  return (
    <>
      <form.AppForm>
        <form.Form label={title}>
          <Section
            id="sign-in"
            icon={<LogInIcon />}
            title={title}
            description={
              <Trans id="admin.oidc-apps.sign-in.description">
                Where this application may send people back to, and what it may
                ask for.
              </Trans>
            }
            dirty={dirty}
            footer={
              dirty ? (
                <ConsequenceFooter
                  consequence={
                    removing.length > 0 ? (
                      <Trans id="admin.oidc-apps.scopes.remove.consequence">
                        Clients still asking for{" "}
                        <ScopeNames names={removing} locale={i18n.locale} />{" "}
                        will be refused until they stop.
                      </Trans>
                    ) : undefined
                  }
                  actionLabel={
                    removing.length > 0 ? (
                      <Plural
                        id="admin.oidc-apps.scopes.remove.action-label"
                        value={removing.length}
                        one="Remove scope and save"
                        other="Remove scopes and save"
                      />
                    ) : (
                      <Trans id="admin.oidc-apps.save">Save</Trans>
                    )
                  }
                  onDiscard={() => form.reset()}
                  pending={submitting || update.isPending}
                />
              ) : undefined
            }
          >
            <div className="flex flex-col gap-6">
              <form.FormError />

              <form.AppField name="redirectUris">
                {() => <RedirectUriList />}
              </form.AppField>

              <form.AppField name="postLogoutRedirectUris">
                {() => (
                  <UriList
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
                {() => <OidcScopesField saved={savedScopes} />}
              </form.AppField>

              <div className="flex flex-col gap-5 border-t pt-6">
                <form.AppField name="requirePkce">
                  {() => <PkceSwitch isPublic={isPublic} />}
                </form.AppField>

                <form.AppField name="requireConsent">
                  {() => <ConsentSwitch />}
                </form.AppField>
              </div>
            </div>
          </Section>
        </form.Form>
      </form.AppForm>

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
    </>
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
