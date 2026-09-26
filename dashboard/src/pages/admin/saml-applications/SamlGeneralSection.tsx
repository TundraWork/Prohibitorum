import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateSamlAppMutationOptions } from "@/api/mutations";
import { samlAppUpdateBody } from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { CopyValue } from "@/components/custom/CopyValue";
import { EntityIconCard } from "@/components/custom/EntityIconCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  displayNameRequired,
  sessionLifetimeInvalid,
} from "@/pages/admin/saml-applications/saml-validation";

type SamlApp = components["schemas"]["SAMLApplicationView"];

/**
 * The application's own icon, which is what a list row draws.
 *
 * A section of its own rather than part of the general card: the icon is the one
 * thing here that is not a field, and pairing it with the form would put an
 * upload control in the middle of a fieldset.
 */
export function SamlIconSection({ app }: { app: SamlApp }) {
  return (
    <Section title={<Trans id="admin.saml-apps.icon">Icon</Trans>}>
      <EntityIconCard
        target={{ kind: "saml", appId: String(app.id) }}
        iconUrl={app.iconUrl}
        name={app.displayName || app.entityId}
      />
    </Section>
  );
}

/**
 * The application's own identity: the Entity ID the service provider published,
 * the name an administrator reads it by, and the two flags that decide what the
 * instance will accept from it.
 *
 * The session lifetime is collected in minutes while the wire carries seconds.
 * An administrator thinks in minutes — "half an hour", "eight hours" — and the
 * form says so, so the conversion happens once, in `samlAppUpdateBody`, rather
 * than at this field. An empty box means no limit: `samlAppUpdateBody` omits
 * `sessionLifetimeSecs` altogether for it, and the server reads the key's absence
 * as "no override". The value is held as text because a number field cannot tell
 * "nothing here" from "the number 0", and while the server treats those two the
 * same, the reader does not — one is a lifetime and the other is not having set
 * one.
 *
 * The Entity ID is shown rather than offered: it is what the service provider
 * published, it is how the provider identifies itself on every request, and
 * changing it would make the two disagree. It is a `CopyValue` because the
 * administrator will paste it into the provider's own configuration.
 */
export function SamlGeneralSection({ app }: { app: SamlApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateSamlAppMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: {
      displayName: app.displayName,
      sessionLifetimeMinutes:
        app.sessionLifetimeSecs === undefined ||
        app.sessionLifetimeSecs === null
          ? ""
          : String(app.sessionLifetimeSecs / 60),
      requireSignedAuthnRequest: app.requireSignedAuthnRequest,
      allowIdpInitiated: app.allowIdpInitiated,
    },
    onSubmit: async ({ value }) => {
      const minutes =
        value.sessionLifetimeMinutes.trim() === ""
          ? null
          : Number(value.sessionLifetimeMinutes.trim());
      if (minutes !== null && (!Number.isFinite(minutes) || minutes < 0)) {
        form.setFieldMeta("sessionLifetimeMinutes", (meta) => ({
          ...meta,
          errors: [sessionLifetimeInvalid],
        }));
        return;
      }

      form.setFieldMeta("sessionLifetimeMinutes", (meta) => ({
        ...meta,
        errors: undefined,
      }));

      try {
        await update.mutateAsync({
          id: app.id,
          body: samlAppUpdateBody(app, {
            displayName: value.displayName.trim(),
            sessionLifetimeMinutes: minutes,
            requireSignedAuthnRequest: value.requireSignedAuthnRequest,
            allowIdpInitiated: value.allowIdpInitiated,
          }),
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: { bad_request: "displayName" },
          locations: {},
        });
      }
    },
  });

  return (
    <Section title={<Trans id="admin.saml-apps.general">General</Trans>}>
      <ConsoleCard>
        <form.AppForm>
          <form.Form
            label={t({ id: "admin.saml-apps.general", message: "General" })}
          >
            <form.FormError />

            <CopyValue
              label={
                <Trans id="admin.saml-apps.field.entity-id">Entity ID</Trans>
              }
              value={app.entityId}
              description={
                <Trans id="admin.saml-apps.general.entity-id.hint">
                  The identifier the service provider publishes. The instance
                  sends it with every response.
                </Trans>
              }
            />

            <form.AppField
              name="displayName"
              validators={{
                onSubmit: ({ value }) =>
                  value.trim() === "" ? displayNameRequired : undefined,
              }}
            >
              {(field) => (
                <field.FormField
                  label={<Trans id="admin.saml-apps.field.name">Name</Trans>}
                  variant="secondary"
                />
              )}
            </form.AppField>

            <form.AppField name="sessionLifetimeMinutes">
              {(field) => (
                <field.NumberField
                  label={
                    <Trans id="admin.saml-apps.field.lifetime">
                      Session lifetime
                    </Trans>
                  }
                  description={
                    <Trans id="admin.saml-apps.field.lifetime.hint">
                      In minutes. Leave it empty to set no limit on how long a
                      session lasts.
                    </Trans>
                  }
                />
              )}
            </form.AppField>

            <form.AppField name="requireSignedAuthnRequest">
              {(field) => (
                <field.SwitchField
                  label={
                    <Trans id="admin.saml-apps.field.signed-request">
                      Require a signed AuthnRequest
                    </Trans>
                  }
                  description={
                    <Trans id="admin.saml-apps.general.signed-request.hint">
                      Refuse a sign-in request the service provider has not
                      signed. Good practice for every provider that can sign
                      one.
                    </Trans>
                  }
                />
              )}
            </form.AppField>

            <form.AppField name="allowIdpInitiated">
              {(field) => (
                <field.SwitchField
                  label={
                    <Trans id="admin.saml-apps.field.idp-initiated">
                      Allow IdP-initiated sign-in
                    </Trans>
                  }
                  description={
                    <Trans id="admin.saml-apps.general.idp-initiated.hint">
                      Let people start at this instance instead of at the
                      service provider.
                    </Trans>
                  }
                />
              )}
            </form.AppField>

            <form.SubmitButton>
              <Trans id="admin.saml-apps.general.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>
    </Section>
  );
}
