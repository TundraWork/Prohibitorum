import { Trans } from "@lingui/react/macro";
import { useEffect } from "react";
import type { ProviderMode } from "@/api/raw-admin-paths";
import { ProviderChoiceField } from "@/pages/admin/identity-providers/ProviderChoiceField";
import { providerModes } from "@/pages/admin/identity-providers/provider-options";

/**
 * What a provider does with someone it has not seen, as a form field.
 *
 * Each mode is a card with its consequence under its name, since the three
 * differ in who ends up with an account, not in anything the reader can see on
 * this page.
 *
 * With sign-in off, "creates accounts" stays on screen but cannot be chosen,
 * and the field says why: that mode exists to back a sign-in button, so choosing
 * it would be a promise the provider cannot keep. Turning sign-in off while
 * that mode is selected resets it to invitation-only, so the field never shows
 * a selection the provider would reject.
 */
export function ProvisioningModeField({
  allowSignIn,
  autoProvisionSelected,
  onAutoProvisionInvalid,
}: {
  allowSignIn: boolean;
  /** Whether the form currently holds "auto_provision". */
  autoProvisionSelected?: boolean;
  /** Called when the selected mode stops being available. */
  onAutoProvisionInvalid?: () => void;
}) {
  useEffect(() => {
    if (!allowSignIn && autoProvisionSelected) onAutoProvisionInvalid?.();
  }, [allowSignIn, autoProvisionSelected, onAutoProvisionInvalid]);

  return (
    <ProviderChoiceField<ProviderMode>
      label={<Trans id="admin.federation.general.mode">Provisioning</Trans>}
      options={providerModes}
      unavailable={
        allowSignIn
          ? undefined
          : {
              values: ["auto_provision"],
              reason: (
                <Trans id="admin.federation.mode.auto.needs-sign-in">
                  Creating accounts requires sign-in to be on.
                </Trans>
              ),
            }
      }
    />
  );
}
