import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PaletteIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateSamlAppMutationOptions } from "@/api/mutations";
import { samlAppUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { EntityIconControl } from "@/components/custom/EntityIconControl";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { displayNameRequired } from "@/pages/admin/saml-applications/saml-validation";

type SamlApp = components["schemas"]["SAMLApplicationView"];

/** The form's values for a stored record, also the baseline after a save. */
function appearanceValues(app: SamlApp) {
  return { displayName: app.displayName };
}

/**
 * How the application is shown in the console: its name and its icon.
 *
 * The form submits through `samlAppUpdateBody`. `PUT` replaces the record, so
 * the sign-in rules and the identity settings go out as they are stored, and an
 * unsaved edit in those sections never leaves with a save here. The icon is its
 * own write, made when the file is chosen, so it is not part of what the footer
 * saves.
 */
export function SamlAppearanceSection({ app }: { app: SamlApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateSamlAppMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: appearanceValues(app),
    onSubmit: async ({ value }) => {
      try {
        const saved = await update.mutateAsync({
          id: app.id,
          body: samlAppUpdateBody(app, {
            displayName: value.displayName.trim(),
          }),
        });
        // The stored record is the new baseline, so the section reads as saved.
        form.reset(appearanceValues(saved));
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: { bad_request: "displayName" },
          locations: {},
        });
      }
    },
  });

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const title = t({ id: "admin.saml-apps.appearance", message: "Appearance" });

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
                actionLabel={<Trans id="admin.saml-apps.save">Save</Trans>}
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
                  value.trim() === "" ? displayNameRequired : undefined,
              }}
            >
              {(field) => (
                <field.FormField
                  label={<Trans id="admin.saml-apps.field.name">Name</Trans>}
                />
              )}
            </form.AppField>

            <div className="flex flex-col gap-2">
              <div className="font-medium text-sm">
                <Trans id="admin.saml-apps.icon">Icon</Trans>
              </div>
              <EntityIconControl
                target={{ kind: "saml", appId: String(app.id) }}
                iconUrl={app.iconUrl}
                name={app.displayName || app.entityId}
              />
            </div>
          </div>
        </Section>
      </form.Form>
    </form.AppForm>
  );
}
