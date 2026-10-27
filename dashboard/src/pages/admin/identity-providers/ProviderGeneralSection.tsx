import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { LogInIcon } from "lucide-react";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { readProviderMode } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import type { ProviderWriteBody } from "@/api/raw-admin-paths";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { ProvisioningModeField } from "@/pages/admin/identity-providers/ProvisioningModeField";

type Provider = components["schemas"]["IdentityProviderView"];

/** The form's values for a stored record, also the baseline after a save. */
function signInValues(provider: Provider) {
  return {
    mode: readProviderMode(provider.mode) ?? "invite_only",
    allowSignIn: provider.allowSignIn,
  };
}

/**
 * What turning sign-in off does, said in the footer where the save is chosen.
 * The dialog that follows asks for the decision and does not say it again.
 *
 * Creating accounts needs a sign-in button, so a provider that was set to
 * create them falls back to invitations; that second change is told here too.
 */
function SignInOffConsequence({ provisioning }: { provisioning: boolean }) {
  return (
    <>
      <Trans id="admin.federation.signin.consequence">
        People who sign in only through this provider can't sign in until an
        administrator sends them a registration link.
      </Trans>
      {provisioning && (
        <>
          {" "}
          <Trans id="admin.federation.signin.consequence.provisioning">
            Provisioning also changes from "Creates accounts" to "Invitation
            only", because creating accounts requires sign-in.
          </Trans>
        </>
      )}
    </>
  );
}

/**
 * Whether people can sign in with the provider, and what it does with someone
 * it has not seen. Both apply to every protocol.
 *
 * Turning sign-in off is confirmed when the form is saved, not when the switch
 * moves: the footer states the consequence and names the action while the
 * switch differs from the saved one, and submitting then opens the dialog with
 * the checked request held beside the form. The write happens from the dialog,
 * and cancelling leaves the reader's choice in the form, unsent.
 *
 * The save goes through `identityProviderUpdateBody`, so the name, the
 * connection and the claims the other sections edit go out as they are stored.
 */
export function ProviderGeneralSection({ provider }: { provider: Provider }) {
  const { t } = useLingui();
  const name = provider.displayName || provider.slug;
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateIdentityProviderMutationOptions(queryClient), true),
  );
  // The request the dialog is confirming, checked and ready to send.
  const [pendingBody, setPendingBody] = useState<ProviderWriteBody | null>(
    null,
  );

  const form = useAppForm({
    defaultValues: signInValues(provider),
    onSubmit: async ({ value }) => {
      const body = identityProviderUpdateBody(provider, {
        // Creating accounts backs a sign-in button, so with sign-in off the
        // mode falls back rather than going out as one the server rejects.
        mode:
          !value.allowSignIn && value.mode === "auto_provision"
            ? "invite_only"
            : value.mode,
        allowSignIn: value.allowSignIn,
      });
      if (provider.allowSignIn && !value.allowSignIn) {
        setPendingBody(body);
        return;
      }
      await save(body);
    },
  });

  async function save(body: ProviderWriteBody) {
    try {
      const saved = await update.mutateAsync({ slug: provider.slug, body });
      // The stored record is the new baseline, so the section reads as saved.
      form.reset(signInValues(saved));
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, { codes: {}, locations: {} });
    }
  }

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const turningOff = useStore(
    form.store,
    (state) => provider.allowSignIn && !state.values.allowSignIn,
  );
  // Turning sign-in off resets "creates accounts" to invitations at once, so
  // the saved mode and the form's differ by the time the footer is read.
  const losesProvisioning = useStore(
    form.store,
    (state) =>
      provider.mode === "auto_provision" && state.values.mode === "invite_only",
  );

  const title = t({
    id: "admin.federation.signin.title",
    message: "Sign-in",
  });

  return (
    <>
      <form.AppForm>
        <form.Form label={title}>
          <Section
            id="sign-in"
            icon={<LogInIcon />}
            title={title}
            description={
              <Trans id="admin.federation.signin.description">
                Choose whether this provider can be used to sign in, and how it
                handles people without a linked account.
              </Trans>
            }
            dirty={dirty}
            footer={
              dirty ? (
                <ConsequenceFooter
                  consequence={
                    turningOff ? (
                      <SignInOffConsequence provisioning={losesProvisioning} />
                    ) : undefined
                  }
                  actionLabel={
                    turningOff ? (
                      <Trans id="admin.federation.signin.action-label">
                        Turn off sign-in and save
                      </Trans>
                    ) : (
                      <Trans id="admin.federation.signin.save">Save</Trans>
                    )
                  }
                  onDiscard={() => form.reset()}
                  // The confirmation carries the spinner when it sends the save.
                  pending={submitting}
                  disabled={update.isPending}
                />
              ) : undefined
            }
          >
            <div className="flex flex-col gap-6">
              <form.FormError />

              <form.AppField name="allowSignIn">
                {(field) => (
                  <field.SwitchField
                    label={
                      <Trans id="admin.federation.general.sign_in">
                        Allow sign-in
                      </Trans>
                    }
                    description={
                      <>
                        {provider.protocol === "vrchat" && (
                          <span className="mb-1 block">
                            <Trans id="admin.federation.general.sign_in.vrchat_hint">
                              VRChat verifies a profile through a link in its
                              bio, which is weaker than an OIDC sign-in. When
                              this is on, anyone who can edit the profile can
                              sign in to the account linked to it.
                            </Trans>
                          </span>
                        )}
                        <span className="block">
                          <Trans id="admin.federation.general.sign_in.hint">
                            When off, this provider can only be used to link
                            accounts and accept invitations, not to sign in.
                          </Trans>
                        </span>
                      </>
                    }
                  />
                )}
              </form.AppField>

              <form.AppField name="mode">
                {() => (
                  <form.Subscribe
                    selector={(state) =>
                      [state.values.allowSignIn, state.values.mode] as const
                    }
                  >
                    {([allowSignIn, mode]) => (
                      <ProvisioningModeField
                        allowSignIn={allowSignIn}
                        autoProvisionSelected={mode === "auto_provision"}
                        onAutoProvisionInvalid={() => {
                          form.setFieldValue("mode", "invite_only");
                        }}
                      />
                    )}
                  </form.Subscribe>
                )}
              </form.AppField>
            </div>
          </Section>
        </form.Form>
      </form.AppForm>

      <ConfirmDialog
        isOpen={pendingBody !== null}
        onOpenChange={(open) => {
          if (!open && !update.isPending) setPendingBody(null);
        }}
        status="warning"
        title={
          <Trans id="admin.federation.general.sign_in.confirm_title">
            Turn off sign-in for {name}?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.federation.general.sign_in.confirm_body">
              The provider will no longer appear on the sign-in page. Sign-in
              can be turned back on at any time.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.federation.general.sign_in.confirm">
            Turn off sign-in
          </Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (pendingBody === null) return;
          // Closed once the write settles either way: a refusal is reported on
          // the form, where the reader can act on it.
          void save(pendingBody).finally(() => setPendingBody(null));
        }}
      />
    </>
  );
}
