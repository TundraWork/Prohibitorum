import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { isCancellation } from "@/api/errors";
import { readProviderMode } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
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
  // The sign-off confirmation gates the submit: the dialog settles it, and a
  // cancelled dialog leaves the form as it was.
  const [confirmingOff, setConfirmingOff] = useState(false);
  const pendingConfirm = useRef<Promise<boolean> | null>(null);
  const settleConfirm = useRef<((confirmed: boolean) => void) | null>(null);
  const form = useAppForm({
    defaultValues: {
      displayName: provider.displayName,
      mode: readProviderMode(provider.mode) ?? "invite_only",
      allowSignIn: provider.allowSignIn,
    },
    onSubmit: async ({ value }) => {
      if (provider.allowSignIn && !value.allowSignIn) {
        pendingConfirm.current = new Promise<boolean>((resolve) => {
          settleConfirm.current = resolve;
        });
        setConfirmingOff(true);
        if (!(await pendingConfirm.current)) return;
      }
      try {
        await update.mutateAsync({
          slug: provider.slug,
          body: identityProviderUpdateBody(provider, {
            displayName: value.displayName.trim(),
            mode: value.allowSignIn
              ? value.mode
              : value.mode === "auto_provision"
                ? "invite_only"
                : value.mode,
            allowSignIn: value.allowSignIn,
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

            <form.AppField name="allowSignIn">
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="admin.federation.general.sign_in">
                      Allow sign-in
                    </Trans>
                  }
                  description={
                    provider.protocol === "vrchat" ? (
                      <Trans id="admin.federation.general.sign_in.vrchat_hint">
                        VRChat proves a profile through a link in its bio, which
                        is weaker than an OIDC sign-in. Once this is on, anyone
                        who can edit the profile can sign in to the account
                        linked to it.
                      </Trans>
                    ) : (
                      <Trans id="admin.federation.general.sign_in.hint">
                        When this is off, this provider can't be used to sign
                        in. People can only link it from their account, or use
                        it to accept an invitation.
                      </Trans>
                    )
                  }
                />
              )}
            </form.AppField>

            <form.AppField name="mode">
              {() => (
                <form.Subscribe
                  selector={(state) => [state.values.allowSignIn] as const}
                >
                  {([allowSignIn]) => (
                    <ProvisioningModeField allowSignIn={allowSignIn} />
                  )}
                </form.Subscribe>
              )}
            </form.AppField>
            <ConfirmDialog
              isOpen={confirmingOff}
              onOpenChange={(open) => {
                if (!open) settleConfirm.current?.(false);
                setConfirmingOff(open);
              }}
              status="warning"
              title={
                <Trans id="admin.federation.general.sign_in.confirm_title">
                  Turn off sign-in?
                </Trans>
              }
              body={
                <Trans id="admin.federation.general.sign_in.confirm_body">
                  Anyone who signs in only through this provider will be locked
                  out until an administrator sends them a reset link.
                </Trans>
              }
              confirmLabel={
                <Trans id="admin.federation.general.sign_in.confirm">
                  Turn off sign-in
                </Trans>
              }
              isPending={update.isPending}
              onConfirm={() => {
                settleConfirm.current?.(true);
                setConfirmingOff(false);
              }}
            />
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
