import { Trans } from "@lingui/react/macro";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";

/** The "stay signed in" choice shared by the sign-in and pairing pages.
 * Controlled by the caller; every sign-in method on the page reads the current
 * state at submit time. */
export function RememberCheckbox({
  isSelected,
  onChange,
  isDisabled,
}: {
  isSelected: boolean;
  onChange: (selected: boolean) => void;
  isDisabled?: boolean;
}) {
  return (
    <Label className="w-fit gap-2.5 text-base/5 sm:text-base/5">
      <Checkbox
        checked={isSelected}
        disabled={isDisabled}
        onCheckedChange={onChange}
      />
      <Trans id="login.remember">Stay signed in on this device</Trans>
    </Label>
  );
}
