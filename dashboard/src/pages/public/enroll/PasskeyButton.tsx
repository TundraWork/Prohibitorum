import { Tooltip } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import { Fingerprint } from "lucide-react";
import type { ReactNode } from "react";
import { Button } from "@/components/custom/Button";

/**
 * The first way to set up a sign-in on the enrollment page and on the page
 * that adds one after a first federated sign-in. A browser or connection
 * without passkeys disables it and says why in its tooltip; a disabled button
 * emits no hover or focus, so the tooltip listens on the trigger around it.
 */
export function PasskeyButton({
  supported,
  isPending,
  isDisabled,
  type = "button",
  onPress,
  children,
}: {
  supported: boolean;
  isPending: boolean;
  isDisabled: boolean;
  /** `submit` when the button sends the form its fields are in. */
  type?: "button" | "submit";
  onPress?: () => void;
  children: ReactNode;
}) {
  const button = (
    <Button
      type={type}
      fullWidth
      isPending={supported && isPending}
      isDisabled={!supported || isDisabled}
      onPress={onPress}
    >
      {({ isPending }) => (
        <>
          {/* The spinner takes the icon's place while it runs. */}
          {!isPending && <Fingerprint aria-hidden="true" />}
          {children}
        </>
      )}
    </Button>
  );
  if (supported) return button;
  return (
    <Tooltip delay={0}>
      <Tooltip.Trigger className="w-full">{button}</Tooltip.Trigger>
      <Tooltip.Content>
        <Trans id="enroll.passkey.unsupported">
          Passkeys aren't available in this browser or connection.
        </Trans>
      </Tooltip.Content>
    </Tooltip>
  );
}
