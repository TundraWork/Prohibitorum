import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import { readProviderMode } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { EntityIconCard } from "@/components/custom/EntityIconCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { ProvisioningModeField } from "@/pages/admin/identity-providers/ProvisioningModeField";

type Provider = components["schemas"]["IdentityProviderView"];

const nameRequired = msg({
  id: "admin.federation.general.name.required",
  message: "Give the provider a name.",
});

/**
 * What the provider is called and what it does with someone it has not seen.
 *
 * The two belong together because they are what the list shows about a
 * provider, and both apply to every protocol. The save sends the whole record
 * through `identityProviderUpdateBody`, so the connection and claims the other
 * sections edit go out as they are stored.
 */
export function ProviderGeneralSection({ provider }: { provider: Provider }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    updateIdentityProviderMutationOptions(queryClient),
  );

  const form = useAppForm({
    defaultValues: {
      displayName: provider.displayName,
      mode: readProviderMode(provider.mode) ?? "invite_only",
    },
    onSubmit: async ({ value }) => {
      try {
        await update.mutateAsync({
          slug: provider.slug,
          body: identityProviderUpdateBody(provider, {
            displayName: value.displayName.trim(),
            mode: provider.protocol === "vrchat" ? "link_only" : value.mode,
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

  const title = t({
    id: "admin.federation.general.title",
    message: "Provider",
  });

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
                  label={<Trans id="admin.federation.general.name">Name</Trans>}
                  variant="secondary"
                />
              )}
            </form.AppField>

            <form.AppField name="mode">
              {() => (
                <ProvisioningModeField
                  isFixed={provider.protocol === "vrchat"}
                />
              )}
            </form.AppField>

            <form.SubmitButton>
              <Trans id="admin.federation.general.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>
    </Section>
  );
}

/** How the provider is shown on the sign-in page and in every list. */
export function ProviderIconSection({ provider }: { provider: Provider }) {
  return (
    <Section title={<Trans id="admin.federation.icon.title">Icon</Trans>}>
      <EntityIconCard
        target={{ kind: "identity-provider", slug: provider.slug }}
        iconUrl={provider.iconUrl}
        name={provider.displayName}
      />
    </Section>
  );
}
