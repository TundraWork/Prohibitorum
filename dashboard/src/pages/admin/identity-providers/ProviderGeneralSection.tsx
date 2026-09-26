import { Description, Label, ListBox, Select } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import type { ProviderMode } from "@/api/raw-admin-paths";
import {
  type IdentityProviderPatch,
  identityProviderUpdateBody,
} from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { EntityIconCard } from "@/components/custom/EntityIconCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type Provider = {
  slug: string;
  displayName: string;
  protocol: string;
  mode: string;
  config: unknown;
  iconUrl?: string | undefined;
};

const nameRequired = msg({
  id: "admin.federation.general.name.required",
  message: "Give the provider a name.",
});

/**
 * What the provider is called and how it treats the accounts it sees.
 *
 * The icon lives here rather than in its own section: it is part of how the
 * provider presents itself everywhere it is listed, and nothing else in these
 * settings groups with it.
 *
 * The provisioning mode is shown read-only for VRChat, which links existing
 * accounts and cannot create any. A select that silently ignores what you pick
 * would be worse than one that says it is fixed.
 */
export function ProviderGeneralSection({ provider }: { provider: Provider }) {
  return (
    <>
      <ProviderNameCard provider={provider} />
      <Section title={<Trans id="admin.federation.icon.title">Icon</Trans>}>
        <EntityIconCard
          target={{ kind: "identity-provider", slug: provider.slug }}
          iconUrl={provider.iconUrl}
          name={provider.displayName}
        />
      </Section>
    </>
  );
}

function ProviderNameCard({ provider }: { provider: Provider }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    updateIdentityProviderMutationOptions(queryClient),
  );

  const form = useAppForm({
    defaultValues: {
      displayName: provider.displayName,
      mode: provider.mode as ProviderMode,
    },
    onSubmit: async ({ value }) => {
      try {
        await update.mutateAsync({
          slug: provider.slug,
          body: identityProviderUpdateBody(provider as never, {
            displayName: value.displayName.trim(),
            mode: value.mode,
          } satisfies IdentityProviderPatch),
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

  const [mode, setMode] = useState(provider.mode as ProviderMode);
  const fixedMode = provider.protocol === "vrchat";

  return (
    <ConsoleCard>
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.federation.general.form",
            message: "Provider",
          })}
        >
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

          <div className="flex flex-col gap-1">
            <Label>
              <Trans id="admin.federation.general.mode">Provisioning</Trans>
            </Label>
            {fixedMode ? (
              <>
                <p className="text-sm text-foreground">
                  {i18n._(
                    msg({
                      id: "admin.federation.mode.link",
                      message: "Links existing accounts",
                    }),
                  )}
                </p>
                <Description className="text-xs text-muted">
                  <Trans id="admin.federation.general.mode.vrchat">
                    VRChat accounts can only be linked, never created.
                  </Trans>
                </Description>
              </>
            ) : (
              <Select
                className="w-full"
                variant="secondary"
                value={mode}
                onChange={(next) => setMode(next as ProviderMode)}
              >
                <Select.Trigger>
                  <Select.Value />
                  <Select.Indicator />
                </Select.Trigger>
                <Select.Popover>
                  <ListBox>
                    <ListBox.Item id="auto_provision" textValue="auto">
                      <Trans id="admin.federation.mode.auto">
                        Creates accounts
                      </Trans>
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                    <ListBox.Item id="invite_only" textValue="invite">
                      <Trans id="admin.federation.mode.invite">
                        Invitation only
                      </Trans>
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                    <ListBox.Item id="link_only" textValue="link">
                      <Trans id="admin.federation.mode.link">
                        Links existing accounts
                      </Trans>
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                  </ListBox>
                </Select.Popover>
              </Select>
            )}
          </div>

          <form.SubmitButton>
            <Trans id="admin.federation.general.save">Save</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}
