import { Trans } from "@lingui/react/macro";
import { Fingerprint } from "lucide-react";
import { type ReactNode, useId } from "react";
import { Button } from "@/components/custom/Button";
import { cn } from "@/lib/utils";

/**
 * The passkey action on every public page that offers one — sign-in, the
 * enrollment page, pairing and the page that adds a sign-in after a first
 * federated one. A browser or connection without passkeys disables it and says
 * why in a line beneath it, where a reader on a touch screen can see it too.
 */
export function PasskeyButton({
  supported,
  isPending,
  isDisabled,
  type = "button",
  onPress,
  unsupportedNote,
  children,
}: {
  supported: boolean;
  isPending: boolean;
  isDisabled: boolean;
  /** `submit` when the button sends the form its fields are in. */
  type?: "button" | "submit";
  onPress?: () => void;
  /** Replaces the default line shown when passkeys are unavailable. */
  unsupportedNote?: ReactNode;
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
        variant={supported ? "default" : "outline"}
        size="xl"
        className="h-12 w-full justify-start gap-3 ps-2 sm:h-12"
        aria-describedby={supported ? undefined : noteId}
      >
        <span
          className={cn(
            "flex size-8 items-center justify-center rounded-md ring-1",
            supported
              ? "bg-primary-foreground/12 ring-primary-foreground/16"
              : "bg-muted text-muted-foreground ring-border",
          )}
        >
          <Fingerprint className="size-5" aria-hidden="true" />
        </span>
        {children}
      </Button>
      {!supported && (
        <p id={noteId} className="text-muted-foreground text-sm">
          {unsupportedNote ?? (
            <Trans id="enroll.passkey.unsupported">
              Passkeys aren't available in this browser or connection.
            </Trans>
          )}
        </p>
      )}
    </div>
  );
}
