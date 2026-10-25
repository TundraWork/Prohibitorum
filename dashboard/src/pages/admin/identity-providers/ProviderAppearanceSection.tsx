import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PaletteIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { EntityIconControl } from "@/components/custom/EntityIconControl";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type Provider = components["schemas"]["IdentityProviderView"];

const nameRequired = msg({
  id: "admin.federation.appearance.name.required",
  message: "Enter a name.",
});

/** The form's values for a stored record, also the baseline after a save. */
function appearanceValues(provider: Provider) {
  return { displayName: provider.displayName };
}

/**
 * How the provider is shown on the sign-in page and in every list: its name and
 * its icon.
 *
 * The form submits through `identityProviderUpdateBody`. `PUT` replaces the
 * record, so the sign-in rules, the connection and the claims go out as they
 * are stored, and an unsaved edit in those sections never leaves with a save
 * here. The icon is its own write, made when the file is chosen, so it is not
 * part of what the footer saves.
 */
export function ProviderAppearanceSection({
  provider,
}: {
  provider: Provider;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateIdentityProviderMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: appearanceValues(provider),
    onSubmit: async ({ value }) => {
      try {
        const saved = await update.mutateAsync({
          slug: provider.slug,
          body: identityProviderUpdateBody(provider, {
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
    id: "admin.federation.appearance.title",
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
                  <Trans id="admin.federation.appearance.save">Save</Trans>
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
                    <Trans id="admin.federation.appearance.name">Name</Trans>
                  }
                />
              )}
            </form.AppField>

            <div className="flex flex-col gap-2">
              <div className="font-medium text-sm">
                <Trans id="admin.federation.appearance.icon">Icon</Trans>
              </div>
              <EntityIconControl
                target={{ kind: "identity-provider", slug: provider.slug }}
                iconUrl={provider.iconUrl}
                name={provider.displayName || provider.slug}
              />
            </div>
          </div>
        </Section>
      </form.Form>
    </form.AppForm>
  );
}
