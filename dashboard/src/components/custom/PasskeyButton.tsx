import { Trans } from "@lingui/react/macro";
import { Fingerprint } from "lucide-react";
import { type ReactNode, useId } from "react";
import { Button } from "@/components/custom/Button";

/**
 * The first way to set up a sign-in on the enrollment page and on the page
 * that adds one after a first federated sign-in. A browser or connection
 * without passkeys disables it and says why in a line beneath it, where a
 * reader on a touch screen can see it too.
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
  const noteId = useId();
  return (
    <div className="flex flex-col gap-2">
      <Button
        type={type}
        pending={supported && isPending}
        disabled={!supported || isDisabled}
        onClick={onPress}
        size="lg"
        className="w-full"
        aria-describedby={supported ? undefined : noteId}
      >
        {/* The spinner takes the icon's place while it runs. */}
        {!(supported && isPending) && <Fingerprint aria-hidden="true" />}
        {children}
      </Button>
      {!supported && (
        <p id={noteId} className="text-muted-foreground text-sm">
          <Trans id="enroll.passkey.unsupported">
            Passkeys aren't available in this browser or connection.
          </Trans>
        </p>
      )}
    </div>
  );
}
