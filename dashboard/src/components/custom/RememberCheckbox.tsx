import { Checkbox } from "@heroui/react";
import { Trans } from "@lingui/react/macro";

/** The login page's "keep me signed in" choice, shared by the password and
 * pairing pages. Controlled by the caller; every sign-in method on the page
 * reads the current state at submit time. Both pages draw it on the public
 * Card, so it takes the surface treatment (`secondary`). */
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
    <Checkbox
      variant="secondary"
      isSelected={isSelected}
      isDisabled={isDisabled}
      onChange={onChange}
    >
      <Checkbox.Content>
        <Checkbox.Control>
          <Checkbox.Indicator />
        </Checkbox.Control>
        <Trans id="login.remember">Keep me signed in</Trans>
      </Checkbox.Content>
    </Checkbox>
  );
}
