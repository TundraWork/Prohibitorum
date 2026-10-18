import { Trans } from "@lingui/react/macro";
import { Checkbox } from "@/components/ui/checkbox";
import { Label } from "@/components/ui/label";

/** The login page's "keep me signed in" choice, shared by the password and
 * pairing pages. Controlled by the caller; every sign-in method on the page
 * reads the current state at submit time. */
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
    <Label className="w-fit">
      <Checkbox
        checked={isSelected}
        disabled={isDisabled}
        onCheckedChange={onChange}
      />
      <Trans id="login.remember">Keep me signed in</Trans>
    </Label>
  );
}
