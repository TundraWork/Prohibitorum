import { Trans } from "@lingui/react/macro";
import type { ProviderMode } from "@/api/raw-admin-paths";
import { OptionSelectField } from "@/components/custom/OptionSelectField";
import { providerModesForSignIn } from "@/pages/admin/identity-providers/provider-options";

/**
 * What a provider does with someone it has not seen, as a form field.
 *
 * Each mode carries its consequence under its name, since the three differ in
 * who ends up with an account, not in anything the reader can see on this page.
 *
 * With sign-in off, "creates accounts" disappears: that mode exists to back a
 * sign-in button, so offering it would be a promise the provider cannot keep.
 * Turning sign-in off while that mode is selected resets it to invitation-only
 * immediately, so the field never shows a selection the provider would reject.
 */
export function ProvisioningModeField({
  allowSignIn,
  autoProvisionSelected,
  onAutoProvisionInvalid,
}: {
  allowSignIn: boolean;
  /** Whether the form currently holds "auto_provision". */
  autoProvisionSelected?: boolean;
  /** Called when the mode list loses the selected value. */
  onAutoProvisionInvalid?: () => void;
}) {
  if (!allowSignIn && autoProvisionSelected && onAutoProvisionInvalid) {
    onAutoProvisionInvalid();
  }
  return (
    <OptionSelectField<ProviderMode>
      label={<Trans id="admin.federation.general.mode">Provisioning</Trans>}
      options={providerModesForSignIn(allowSignIn)}
    />
  );
}
