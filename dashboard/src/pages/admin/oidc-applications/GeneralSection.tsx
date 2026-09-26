import { Tooltip } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateOidcAppMutationOptions } from "@/api/mutations";
import { oidcAppUpdateBody } from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { CopyValue } from "@/components/custom/CopyValue";
import { EntityIconCard } from "@/components/custom/EntityIconCard";
import { Section } from "@/components/custom/Section";
import { formatLines, lineProblemMessage, parseLines } from "@/forms/lines";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type OidcApp = components["schemas"]["OIDCApplicationView"];

const nameRequired = msg({
  id: "admin.oidc-apps.field.name.required",
  message: "Enter a name.",
});

const redirectInvalid = msg({
  id: "admin.oidc-apps.field.redirect.invalid",
  message: "Use an absolute http or https address.",
});

const redirectRequired = msg({
  id: "admin.oidc-apps.field.redirect.required",
  message: "A client needs at least one redirect URI to sign in with.",
});

const scopesRequired = msg({
  id: "admin.oidc-apps.field.scopes.required",
  message: "Enter at least one scope.",
});

/**
 * Why one address is not usable.
 *
 * The server stores these as given and never checks them, so this is the only
 * check either one of them gets before a client's sign-in breaks. The rule is
 * "absolute http(s), with a host, without credentials" and no tighter: a native
 * client legitimately loops back to `http://127.0.0.1`, so requiring https would
 * refuse a working client.
 */
function absoluteHttpUrl(value: string) {
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return redirectInvalid;
  }
  if (url.protocol !== "http:" && url.protocol !== "https:") {
    return redirectInvalid;
  }
  if (url.host === "" || url.username !== "" || url.password !== "") {
    return redirectInvalid;
  }
  return undefined;
}

/**
 * What the application is called and where the instance sends the browser.
 *
 * The Client ID is shown but never editable: the server takes it only on create,
 * and by the time anyone reads this page every client is already configured with
 * it. `CopyValue` is how it comes back out, which is the one thing a reader
 * opens this card to do.
 *
 * The whole form submits through `oidcAppUpdateBody`. `PUT` replaces the record
 * rather than patching it, so the fields this card does not draw — the disabled
 * flag and the launch URL — have to be carried from the saved view. Building
 * that merge per card is exactly how one of them ends up cleared by a save from
 * somewhere else, which is the reason the module exists.
 */
export function GeneralSection({ app }: { app: OidcApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateOidcAppMutationOptions(queryClient));

  // `sub` is never in this set as a client authentication method, only as a
  // subject source; `none` is the one value that means "no secret at all".
  const isPublic = app.clientAuthMethod === "none";

  const form = useAppForm({
    defaultValues: {
      displayName: app.displayName,
      launchUrl: app.launchUrl ?? "",
      redirectUris: formatLines(app.redirectUris ?? []),
      postLogoutRedirectUris: formatLines(app.postLogoutRedirectUris ?? []),
      allowedScopes: (app.allowedScopes ?? []).join(" "),
      requirePkce: app.requirePkce,
      requireConsent: app.requireConsent,
    },
    onSubmit: async ({ value }) => {
      const redirectUris = parseLines(value.redirectUris, absoluteHttpUrl);
      if (!redirectUris.ok) {
        form.setFieldMeta("redirectUris", (meta) => ({
          ...meta,
          errors: [lineProblemMessage(redirectUris.problem)],
        }));
        return;
      }
      if (redirectUris.values.length === 0) {
        form.setFieldMeta("redirectUris", (meta) => ({
          ...meta,
          errors: [redirectRequired],
        }));
        return;
      }
      const postLogout = parseLines(
        value.postLogoutRedirectUris,
        absoluteHttpUrl,
      );
      if (!postLogout.ok) {
        form.setFieldMeta("postLogoutRedirectUris", (meta) => ({
          ...meta,
          errors: [lineProblemMessage(postLogout.problem)],
        }));
        return;
      }
      const launchUrl = value.launchUrl.trim();
      if (launchUrl !== "") {
        const problem = absoluteHttpUrl(launchUrl);
        if (problem !== undefined) {
          form.setFieldMeta("launchUrl", (meta) => ({
            ...meta,
            errors: [problem],
          }));
          return;
        }
      }

      try {
        await update.mutateAsync({
          clientId: app.clientId,
          // A public client must keep PKCE on: the server refuses `false` for
          // one, and the switch is locked below rather than merely defaulted.
          body: oidcAppUpdateBody(app, {
            displayName: value.displayName.trim(),
            launchUrl: launchUrl === "" ? null : launchUrl,
            redirectUris: redirectUris.values,
            postLogoutRedirectUris: postLogout.values,
            allowedScopes: value.allowedScopes.split(/\s+/).filter(Boolean),
            requirePkce: isPublic ? true : value.requirePkce,
            requireConsent: value.requireConsent,
          }),
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: { bad_request: "launchUrl" },
          locations: {},
        });
      }
    },
  });

  return (
    <Section title={<Trans id="admin.oidc-apps.general">General</Trans>}>
      <ConsoleCard>
        <form.AppForm>
          <form.Form
            label={t({ id: "admin.oidc-apps.general", message: "General" })}
          >
            <form.FormError />

            <CopyValue
              value={app.clientId}
              label={
                <Trans id="admin.oidc-apps.field.client-id">Client ID</Trans>
              }
              description={
                <Trans id="admin.oidc-apps.field.client-id.hint">
                  The client is configured with this value; it cannot change.
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
                  label={<Trans id="admin.oidc-apps.field.name">Name</Trans>}
                  variant="secondary"
                />
              )}
            </form.AppField>

            <form.AppField
              name="launchUrl"
              validators={{
                onSubmit: ({ value }) => {
                  const launchUrl = value.trim();
                  return launchUrl === ""
                    ? undefined
                    : absoluteHttpUrl(launchUrl);
                },
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="admin.oidc-apps.field.launch">Launch URL</Trans>
                  }
                  description={
                    <Trans id="admin.oidc-apps.field.launch.hint">
                      Where the console links when the application is opened
                      from here. Leave it empty to link nowhere.
                    </Trans>
                  }
                  autoComplete="off"
                  spellCheck={false}
                  variant="secondary"
                />
              )}
            </form.AppField>

            <form.AppField
              name="redirectUris"
              validators={{
                onSubmit: ({ value }) => {
                  const parsed = parseLines(value, absoluteHttpUrl);
                  if (!parsed.ok) return lineProblemMessage(parsed.problem);
                  return parsed.values.length === 0
                    ? redirectRequired
                    : undefined;
                },
              }}
            >
              {(field) => (
                <field.TextAreaField
                  label={
                    <Trans id="admin.oidc-apps.field.redirects">
                      Redirect URIs
                    </Trans>
                  }
                  description={
                    <Trans id="admin.oidc-apps.field.redirects.hint">
                      One per line. Absolute http or https addresses.
                    </Trans>
                  }
                  rows={3}
                  spellCheck={false}
                  variant="secondary"
                />
              )}
            </form.AppField>

            <form.AppField
              name="postLogoutRedirectUris"
              validators={{
                onSubmit: ({ value }) => {
                  const parsed = parseLines(value, absoluteHttpUrl);
                  return parsed.ok
                    ? undefined
                    : lineProblemMessage(parsed.problem);
                },
              }}
            >
              {(field) => (
                <field.TextAreaField
                  label={
                    <Trans id="admin.oidc-apps.field.post-logout">
                      Post-logout redirect URIs
                    </Trans>
                  }
                  description={
                    <Trans id="admin.oidc-apps.field.post-logout.hint">
                      One per line, where the client may return after signing
                      out. Empty means it may not.
                    </Trans>
                  }
                  rows={2}
                  spellCheck={false}
                  variant="secondary"
                />
              )}
            </form.AppField>

            <form.AppField
              name="allowedScopes"
              validators={{
                onSubmit: ({ value }) =>
                  value.trim() === "" ? scopesRequired : undefined,
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="admin.oidc-apps.field.scopes">Scopes</Trans>
                  }
                  description={
                    <Trans id="admin.oidc-apps.field.scopes.hint">
                      Separated by spaces, as a client sends them.
                    </Trans>
                  }
                  autoComplete="off"
                  spellCheck={false}
                  variant="secondary"
                />
              )}
            </form.AppField>

            {isPublic ? (
              // Locked rather than hidden: a reader wondering why a public client
              // has no choice gets the answer beside the switch.
              <Tooltip delay={0}>
                <Tooltip.Trigger>
                  <div className="w-fit">
                    <form.AppField name="requirePkce">
                      {(field) => (
                        <field.SwitchField
                          label={
                            <Trans id="admin.oidc-apps.field.pkce">
                              Require PKCE
                            </Trans>
                          }
                          isDisabled
                        />
                      )}
                    </form.AppField>
                  </div>
                </Tooltip.Trigger>
                <Tooltip.Content>
                  <Trans id="admin.oidc-apps.field.pkce.public">
                    A public client always proves the request it started.
                  </Trans>
                </Tooltip.Content>
              </Tooltip>
            ) : (
              <form.AppField name="requirePkce">
                {(field) => (
                  <field.SwitchField
                    label={
                      <Trans id="admin.oidc-apps.field.pkce">
                        Require PKCE
                      </Trans>
                    }
                  />
                )}
              </form.AppField>
            )}

            <form.AppField name="requireConsent">
              {(field) => (
                <field.SwitchField
                  label={
                    <Trans id="admin.oidc-apps.field.consent">
                      Require consent
                    </Trans>
                  }
                  description={
                    <Trans id="admin.oidc-apps.field.consent.hint">
                      Ask the account to approve the scopes before signing them
                      in.
                    </Trans>
                  }
                />
              )}
            </form.AppField>

            <form.SubmitButton>
              <Trans id="admin.oidc-apps.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>
    </Section>
  );
}

/**
 * The application's icon, drawn at the size the list rows use so what is
 * arranged here is what a row will show.
 */
export function OidcIconSection({ app }: { app: OidcApp }) {
  return (
    <Section title={<Trans id="admin.oidc-apps.icon">Icon</Trans>}>
      <EntityIconCard
        target={{ kind: "oidc", appId: app.clientId }}
        iconUrl={app.iconUrl}
        name={app.displayName || app.clientId}
      />
    </Section>
  );
}
