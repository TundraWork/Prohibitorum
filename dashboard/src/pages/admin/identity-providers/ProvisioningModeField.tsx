import { Trans, useLingui } from "@lingui/react/macro";
import { useEffect, useId } from "react";
import type { ProviderMode } from "@/api/raw-admin-paths";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { FormMessages } from "@/components/custom/FormMessages";
import { useFieldBinding } from "@/components/custom/use-field-binding";
import { FieldError } from "@/components/ui/field";
import { RadioGroup } from "@/components/ui/radio-group";
import { providerModes } from "@/pages/admin/identity-providers/provider-options";

/**
 * What a provider does with someone it has not seen, as a form field.
 *
 * Each mode is a card with its consequence under its name, since the three
 * differ in who ends up with an account, not in anything the reader can see on
 * this page.
 *
 * With sign-in off, "creates accounts" stays on screen but cannot be chosen,
 * and the card says why: that mode exists to back a sign-in button, so choosing
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
  const { i18n } = useLingui();
  const { field, errors, invalid, disabled, onChange } =
    useFieldBinding<ProviderMode>(false);
  const labelId = useId();

  useEffect(() => {
    if (!allowSignIn && autoProvisionSelected) onAutoProvisionInvalid?.();
  }, [allowSignIn, autoProvisionSelected, onAutoProvisionInvalid]);

  return (
    <div className="flex flex-col gap-2">
      <span id={labelId} className="font-medium text-sm">
        <Trans id="admin.federation.general.mode">Provisioning</Trans>
      </span>
      <RadioGroup
        aria-labelledby={labelId}
        name={field.name}
        value={field.state.value}
        disabled={disabled}
        onValueChange={(next) => onChange(next as ProviderMode)}
      >
        {providerModes.map((option) => {
          const unavailable = option.value === "auto_provision" && !allowSignIn;
          return (
            <ChoiceTile
              key={option.value}
              layout="list"
              value={option.value}
              disabled={unavailable}
              label={i18n._(option.label)}
              description={
                <>
                  {i18n._(option.description)}
                  {unavailable && (
                    <span className="mt-0.5 block">
                      <Trans id="admin.federation.mode.auto.needs-sign-in">
                        Turn sign-in on to create accounts.
                      </Trans>
                    </span>
                  )}
                </>
              }
            />
          );
        })}
      </RadioGroup>
      {invalid && (
        <FieldError match>
          <FormMessages errors={errors} />
        </FieldError>
      )}
    </div>
  );
}
