import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PaletteIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateForwardAuthAppMutationOptions } from "@/api/mutations";
import { forwardAuthAppUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { EntityIconControl } from "@/components/custom/EntityIconControl";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

const nameRequired = msg({
  id: "admin.forward-auth-apps.field.name.required",
  message: "Enter a name.",
});

/** The form's values for a stored record, also the baseline after a save. */
function appearanceValues(app: ForwardAuthApp) {
  return { displayName: app.displayName };
}

/**
 * How the application is shown in the console and on the account's own
 * application list: its name and its icon.
 *
 * The form submits through `forwardAuthAppUpdateBody`. `PUT` replaces the
 * record, so the hostname goes out as it is stored and an unsaved edit in the
 * service section never leaves with a save here. The icon is its own write,
 * made when the file is chosen, so it is not part of what the footer saves.
 */
export function ForwardAuthAppearanceSection({ app }: { app: ForwardAuthApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateForwardAuthAppMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: appearanceValues(app),
    onSubmit: async ({ value }) => {
      try {
        const saved = await update.mutateAsync({
          clientId: app.clientId,
          body: forwardAuthAppUpdateBody(app, {
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

  const title = t({
    id: "admin.forward-auth-apps.appearance",
    message: "Appearance",
  });

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
                actionLabel={
                  <Trans id="admin.forward-auth-apps.save">Save</Trans>
                }
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
                  label={
                    <Trans id="admin.forward-auth-apps.field.name">Name</Trans>
                  }
                />
              )}
            </form.AppField>

            <div className="flex flex-col gap-2">
              <div className="font-medium text-sm">
                <Trans id="admin.forward-auth-apps.icon">Icon</Trans>
              </div>
              <EntityIconControl
                target={{ kind: "forward_auth", appId: app.clientId }}
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
