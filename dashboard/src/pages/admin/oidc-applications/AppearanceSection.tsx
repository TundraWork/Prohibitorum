import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateOidcAppMutationOptions } from "@/api/mutations";
import { oidcAppUpdateBody } from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { EntityIconCard } from "@/components/custom/EntityIconCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { absoluteHttpUrlProblem } from "@/pages/admin/oidc-applications/oidc-validation";

type OidcApp = components["schemas"]["OIDCApplicationView"];

const nameRequired = msg({
  id: "admin.oidc-apps.field.name.required",
  message: "Enter a name.",
});

/**
 * How the application is shown in the console: its name, and where the console
 * links when it is opened from here.
 *
 * The form submits through `oidcAppUpdateBody`. `PUT` replaces the record, so
 * the client configuration and sign-in rules go out as they are stored, and an
 * unsaved edit in one of those sections never leaves with a save here.
 */
export function AppearanceSection({ app }: { app: OidcApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateOidcAppMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: {
      displayName: app.displayName,
      launchUrl: app.launchUrl ?? "",
    },
    onSubmit: async ({ value }) => {
      const launchUrl = value.launchUrl.trim();
      try {
        await update.mutateAsync({
          clientId: app.clientId,
          body: oidcAppUpdateBody(app, {
            displayName: value.displayName.trim(),
            launchUrl: launchUrl === "" ? null : launchUrl,
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

  const title = t({ id: "admin.oidc-apps.appearance", message: "Appearance" });

  return (
    <Section title={title}>
      <ConsoleCard>
        <form.AppForm>
          <form.Form label={title}>
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
                    : absoluteHttpUrlProblem(launchUrl);
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
