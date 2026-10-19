import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PaletteIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateOidcAppMutationOptions } from "@/api/mutations";
import { oidcAppUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { EntityIconControl } from "@/components/custom/EntityIconCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { absoluteHttpUrlProblem } from "@/pages/admin/oidc-applications/oidc-validation";

type OidcApp = components["schemas"]["OIDCApplicationView"];

const nameRequired = msg({
  id: "admin.oidc-apps.field.name.required",
  message: "Enter a name.",
});

/** The form's values for a stored record, also the baseline after a save. */
function appearanceValues(app: OidcApp) {
  return { displayName: app.displayName, launchUrl: app.launchUrl ?? "" };
}

/**
 * How the application is shown in the console: its name, where the console
 * links when it is opened from here, and its icon.
 *
 * The form submits through `oidcAppUpdateBody`. `PUT` replaces the record, so
 * the sign-in fields and the identity settings go out as they are stored, and
 * an unsaved edit in one of those sections never leaves with a save here. The
 * icon is its own write, made when the file is chosen, so it is not part of
 * what the footer saves.
 */
export function AppearanceSection({ app }: { app: OidcApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateOidcAppMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: appearanceValues(app),
    onSubmit: async ({ value }) => {
      const launchUrl = value.launchUrl.trim();
      try {
        const saved = await update.mutateAsync({
          clientId: app.clientId,
          body: oidcAppUpdateBody(app, {
            displayName: value.displayName.trim(),
            launchUrl: launchUrl === "" ? null : launchUrl,
          }),
        });
        // The stored record is the new baseline, so the section reads as saved.
        form.reset(appearanceValues(saved));
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: { bad_request: "launchUrl" },
          locations: {},
        });
      }
    },
  });

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const title = t({ id: "admin.oidc-apps.appearance", message: "Appearance" });

  return (
    <form.AppForm>
      <form.Form label={title}>
        <Section
          id="appearance"
          icon={<PaletteIcon />}
          title={title}
          dirty={dirty}
          footer={
            dirty ? (
              <ConsequenceFooter
                actionLabel={<Trans id="admin.oidc-apps.save">Save</Trans>}
                onDiscard={() => form.reset()}
                pending={submitting || update.isPending}
              />
            ) : undefined
          }
        >
          <div className="flex flex-col gap-6">
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
                />
              )}
            </form.AppField>

            <div className="flex flex-col gap-2">
              <div className="font-medium text-sm">
                <Trans id="admin.oidc-apps.icon">Icon</Trans>
              </div>
              <EntityIconControl
                target={{ kind: "oidc", appId: app.clientId }}
                iconUrl={app.iconUrl}
                name={app.displayName || app.clientId}
              />
            </div>
          </div>
        </Section>
      </form.Form>
    </form.AppForm>
  );
}
