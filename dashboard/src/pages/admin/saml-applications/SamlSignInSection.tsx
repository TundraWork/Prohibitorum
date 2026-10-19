import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LogInIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateSamlAppMutationOptions } from "@/api/mutations";
import { samlAppUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { sessionLifetimeInvalid } from "@/pages/admin/saml-applications/saml-validation";

type SamlApp = components["schemas"]["SAMLApplicationView"];

/**
 * The form's values for a stored record, also the baseline after a save.
 *
 * The lifetime is held as text: a number field cannot tell "nothing here" from
 * "the number 0", and while the server treats those two the same, the reader
 * does not. One is a lifetime and the other is not having set one.
 */
function signInValues(app: SamlApp) {
  return {
    sessionLifetimeMinutes:
      app.sessionLifetimeSecs === undefined || app.sessionLifetimeSecs === null
        ? ""
        : String(app.sessionLifetimeSecs / 60),
    requireSignedAuthnRequest: app.requireSignedAuthnRequest,
    allowIdpInitiated: app.allowIdpInitiated,
  };
}

/**
 * How people sign in to the service provider through this instance: how long a
 * session lasts, whether the provider must sign its requests, and whether
 * people may start at this instance instead of at the provider.
 *
 * The lifetime is collected in minutes while the wire carries seconds. An
 * administrator thinks in minutes, so the conversion happens once, in
 * `samlAppUpdateBody`, and an empty box means no limit: the key is omitted and
 * the server reads its absence as "no override".
 *
 * The form submits through `samlAppUpdateBody`. `PUT` replaces the record, so
 * the name, the NameID format and the attribute map go out as they are stored,
 * and an unsaved edit in those sections never leaves with a save here. A failed
 * save is shown on the section alone.
 */
export function SamlSignInSection({ app }: { app: SamlApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateSamlAppMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: signInValues(app),
    onSubmit: async ({ value }) => {
      const minutes =
        value.sessionLifetimeMinutes.trim() === ""
          ? null
          : Number(value.sessionLifetimeMinutes.trim());
      if (minutes !== null && (!Number.isFinite(minutes) || minutes < 0)) {
        form.setFieldMeta("sessionLifetimeMinutes", (meta) => ({
          ...meta,
          errorMap: { ...meta.errorMap, onSubmit: [sessionLifetimeInvalid] },
        }));
        return;
      }

      try {
        const saved = await update.mutateAsync({
          id: app.id,
          body: samlAppUpdateBody(app, {
            sessionLifetimeMinutes: minutes,
            requireSignedAuthnRequest: value.requireSignedAuthnRequest,
            allowIdpInitiated: value.allowIdpInitiated,
          }),
        });
        // The stored record is the new baseline, so the section reads as saved.
        form.reset(signInValues(saved));
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, { codes: {}, locations: {} });
      }
    },
  });

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const title = t({ id: "admin.saml-apps.sign-in.title", message: "Sign-in" });

  return (
    <form.AppForm>
      <form.Form label={title}>
        <Section
          id="sign-in"
          icon={<LogInIcon />}
          title={title}
          description={
            <Trans id="admin.saml-apps.sign-in.description">
              How long a session lasts, and what the service provider has to
              send.
            </Trans>
          }
          dirty={dirty}
          footer={
            dirty ? (
              <ConsequenceFooter
                actionLabel={<Trans id="admin.saml-apps.save">Save</Trans>}
                onDiscard={() => form.reset()}
                pending={submitting || update.isPending}
              />
            ) : undefined
          }
        >
          <div className="flex flex-col gap-6">
            <form.FormError />

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
                  className="max-w-lg"
                />
              )}
            </form.AppField>

            <div className="flex flex-col gap-5 border-t pt-6">
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
            </div>
          </div>
        </Section>
      </form.Form>
    </form.AppForm>
  );
}
