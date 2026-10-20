import { Trans } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRoundIcon } from "lucide-react";
import { useId } from "react";
import { removeUnsplashKeyMutationOptions } from "@/api/mutations";
import { Button } from "@/components/custom/Button";

/**
 * The saved access key, which is never shown again: replace it with a new
 * one, or remove it. Removing is its own write and happens at once. It cannot
 * be removed while the saved background is Unsplash, and the reason sits
 * beside the button rather than in a tooltip, where a touch screen and a
 * screen reader find it.
 */
export function SavedUnsplashKey({
  inUse,
  disabled,
  replacing,
  onReplace,
  onKeep,
}: {
  /** The saved background is Unsplash, so the key is in use. */
  inUse: boolean;
  disabled: boolean;
  /** A new key is being entered, so the saved one can be kept instead. */
  replacing: boolean;
  onReplace: () => void;
  /** Gives up the new key and leaves the saved one as it was. */
  onKeep: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useMutation(removeUnsplashKeyMutationOptions(queryClient));
  const reasonId = useId();
  return (
    <div className="flex flex-col gap-2">
      <span className="font-medium text-sm">
        <Trans id="settings.sign-in.unsplash.key">Access key</Trans>
      </span>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <span className="flex items-center gap-2 text-sm">
          <KeyRoundIcon
            aria-hidden="true"
            className="size-4 text-muted-foreground"
          />
          <Trans id="settings.sign-in.unsplash.key.saved">
            Access key saved
          </Trans>
        </span>
        <div className="flex gap-2">
          {replacing ? (
            <Button
              disabled={disabled}
              onClick={onKeep}
              variant="outline"
              size="sm"
            >
              <Trans id="settings.sign-in.unsplash.key.keep">
                Keep the saved key
              </Trans>
            </Button>
          ) : (
            <>
              <Button
                disabled={disabled}
                onClick={onReplace}
                variant="outline"
                size="sm"
              >
                <Trans id="settings.sign-in.unsplash.key.replace">
                  Replace
                </Trans>
              </Button>
              <Button
                disabled={inUse || disabled}
                pending={remove.isPending}
                aria-describedby={inUse ? reasonId : undefined}
                onClick={() => remove.mutate()}
                variant="destructive-outline"
                size="sm"
              >
                <Trans id="settings.sign-in.unsplash.key.remove">Remove</Trans>
              </Button>
            </>
          )}
        </div>
      </div>
      {inUse && !replacing && (
        <p id={reasonId} className="text-muted-foreground text-sm">
          <Trans id="settings.sign-in.unsplash.key.in_use">
            Switch the background to another source and save before removing the
            key.
          </Trans>
        </p>
      )}
    </div>
  );
}
