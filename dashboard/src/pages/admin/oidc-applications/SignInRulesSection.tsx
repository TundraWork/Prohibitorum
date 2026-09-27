import { Tooltip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateOidcAppMutationOptions } from "@/api/mutations";
import { oidcAppUpdateBody } from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type OidcApp = components["schemas"]["OIDCApplicationView"];

/**
 * What the instance asks of a client, and of the account, before it hands over
 * a code: PKCE, and the account's consent to the scopes.
 *
 * Its own section and save, submitted through `oidcAppUpdateBody` so the
 * fields the other sections draw go out as they are stored.
 */
export function SignInRulesSection({ app }: { app: OidcApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateOidcAppMutationOptions(queryClient));

  // `none` is the one client authentication method that means "no secret at
  // all", which is what makes a client public.
  const isPublic = app.clientAuthMethod === "none";

  const form = useAppForm({
    defaultValues: {
      requirePkce: app.requirePkce,
      requireConsent: app.requireConsent,
    },
    onSubmit: async ({ value }) => {
      try {
        await update.mutateAsync({
          clientId: app.clientId,
          // A public client must keep PKCE on: the server refuses `false` for
          // one, and the switch is locked below rather than merely defaulted.
          body: oidcAppUpdateBody(app, {
            requirePkce: isPublic ? true : value.requirePkce,
            requireConsent: value.requireConsent,
          }),
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, { codes: {}, locations: {} });
      }
    },
  });

  const title = t({ id: "admin.oidc-apps.sign-in", message: "Sign-in rules" });
  const pkceLabel = <Trans id="admin.oidc-apps.field.pkce">Require PKCE</Trans>;

  return (
    <Section title={title}>
      <ConsoleCard>
        <form.AppForm>
          <form.Form label={title}>
            <form.FormError />

            {isPublic ? (
              // Locked rather than hidden: a reader wondering why a public client
              // has no choice gets the answer beside the switch.
              <Tooltip delay={0}>
                {/* Held to the switch's width, so the focus ring and the
                    tooltip's anchor are the switch rather than the row. */}
                <Tooltip.Trigger className="w-fit">
                  <form.AppField name="requirePkce">
                    {(field) => (
                      <field.SwitchField label={pkceLabel} isDisabled />
                    )}
                  </form.AppField>
                </Tooltip.Trigger>
                <Tooltip.Content>
                  <Trans id="admin.oidc-apps.field.pkce.public">
                    A public client always proves the request it started.
                  </Trans>
                </Tooltip.Content>
              </Tooltip>
            ) : (
              <form.AppField name="requirePkce">
                {(field) => <field.SwitchField label={pkceLabel} />}
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
