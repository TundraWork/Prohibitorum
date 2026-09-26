import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import { readOidcProviderConfig } from "@/api/federation";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import type { OidcProviderConfig, ProviderMode } from "@/api/raw-admin-paths";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { lineProblemMessage, parseLines } from "@/forms/lines";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type Provider = {
  slug: string;
  displayName: string;
  mode: string;
  config: unknown;
};

const domainsInvalid = msg({
  id: "admin.federation.claims.domains.invalid",
  message: "Enter a domain such as example.com, without a scheme or a path.",
});

/**
 * Which accounts a provider may create, and how its claims become an account's
 * own fields.
 *
 * The allowed domains are the gate: an empty list means every address the
 * provider vouches for is accepted, which is why the field says so rather than
 * leaving a blank box to be read as "none". The claim names decide what fills in
 * the account's username, display name, email and picture; getting one wrong
 * produces an account with a blank name rather than an error, so each has its
 * own field and the server's default is shown as the starting value.
 */
export function ProviderClaimsSection({ provider }: { provider: Provider }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    updateIdentityProviderMutationOptions(queryClient),
  );

  const saved = readOidcProviderConfig(provider.config);

  const form = useAppForm({
    defaultValues: {
      allowedDomains: (saved?.allowedDomains ?? []).join("\n"),
      requireVerifiedEmail: saved?.requireVerifiedEmail ?? true,
      usernameClaim: saved?.usernameClaim ?? "preferred_username",
      displayNameClaim: saved?.displayNameClaim ?? "name",
      emailClaim: saved?.emailClaim ?? "email",
      pictureClaim: saved?.pictureClaim ?? "picture",
      subjectClaim: saved?.subjectClaim ?? "sub",
    },
    onSubmit: async ({ value }) => {
      const domains = parseLines(value.allowedDomains, (domain) =>
        isDomain(domain) ? undefined : domainsInvalid,
      );
      if (!domains.ok) return;

      const base = saved ?? ({} as OidcProviderConfig);
      const next: OidcProviderConfig = {
        ...base,
        allowedDomains: domains.values,
        requireVerifiedEmail: value.requireVerifiedEmail,
        usernameClaim: value.usernameClaim.trim(),
        displayNameClaim: value.displayNameClaim.trim(),
        emailClaim: value.emailClaim.trim(),
        pictureClaim: value.pictureClaim.trim(),
        subjectClaim: value.subjectClaim.trim(),
      };

      try {
        await update.mutateAsync({
          slug: provider.slug,
          body: identityProviderUpdateBody(provider as never, {
            config: next,
            displayName: provider.displayName,
            mode: provider.mode as ProviderMode,
          }),
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: { bad_request: "allowedDomains" },
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
            id: "admin.federation.claims.form",
            message: "Accounts and claims",
          })}
        >
          <form.FormError />

          <form.AppField
            name="allowedDomains"
            validators={{
              onSubmit: ({ value }) => {
                const parsed = parseLines(value, (domain) =>
                  isDomain(domain) ? undefined : domainsInvalid,
                );
                return parsed.ok
                  ? undefined
                  : lineProblemMessage(parsed.problem);
              },
            }}
          >
            {(field) => (
              <field.TextAreaField
                label={
                  <Trans id="admin.federation.claims.domains">
                    Allowed email domains
                  </Trans>
                }
                description={
                  <Trans id="admin.federation.claims.domains.hint">
                    One per line. Leave empty to accept every domain.
                  </Trans>
                }
                rows={3}
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="requireVerifiedEmail">
            {(field) => (
              <field.SwitchField
                label={
                  <Trans id="admin.federation.claims.verified">
                    Require a verified email address
                  </Trans>
                }
              />
            )}
          </form.AppField>

          <form.AppField name="usernameClaim">
            {(field) => (
              <field.FormField
                label={
                  <Trans id="admin.federation.claims.username">Username</Trans>
                }
                autoComplete="off"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="displayNameClaim">
            {(field) => (
              <field.FormField
                label={
                  <Trans id="admin.federation.claims.display-name">
                    Display name
                  </Trans>
                }
                autoComplete="off"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="emailClaim">
            {(field) => (
              <field.FormField
                label={<Trans id="admin.federation.claims.email">Email</Trans>}
                autoComplete="off"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="pictureClaim">
            {(field) => (
              <field.FormField
                label={
                  <Trans id="admin.federation.claims.picture">Picture</Trans>
                }
                autoComplete="off"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="subjectClaim">
            {(field) => (
              <field.FormField
                label={
                  <Trans id="admin.federation.claims.subject">Subject</Trans>
                }
                autoComplete="off"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.SubmitButton>
            <Trans id="admin.federation.claims.save">Save</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}

/** A bare domain: labels of letters, digits and hyphens, at least one dot. */
function isDomain(value: string): boolean {
  return /^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$/i.test(value);
}
