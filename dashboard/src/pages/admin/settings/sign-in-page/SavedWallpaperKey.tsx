import { Trans } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { KeyRoundIcon } from "lucide-react";
import { useId } from "react";
import {
  removePixabayKeyMutationOptions,
  removeUnsplashKeyMutationOptions,
} from "@/api/mutations";
import { Button } from "@/components/custom/Button";

/**
 * A picture service's saved key, which is never shown again: replace it with
 * a new one, or remove it. Removing is its own write and happens at once. It
 * cannot be removed while the saved background is that service, and the
 * reason sits beside the button rather than in a tooltip, where a touch
 * screen and a screen reader find it.
 */
export function SavedWallpaperKey({
  source,
  inUse,
  disabled,
  replacing,
  onReplace,
  onKeep,
}: {
  /** Which service's key this row holds: its name and its delete path. */
  source: "unsplash" | "pixabay";
  /** The saved background is that service, so the key is in use. */
  inUse: boolean;
  disabled: boolean;
  /** A new key is being entered, so the saved one can be kept instead. */
  replacing: boolean;
  onReplace: () => void;
  /** Gives up the new key and leaves the saved one as it was. */
  onKeep: () => void;
}) {
  const queryClient = useQueryClient();
  const removeOptions =
    source === "unsplash"
      ? removeUnsplashKeyMutationOptions(queryClient)
      : removePixabayKeyMutationOptions(queryClient);
  const remove = useMutation(removeOptions);
  const reasonId = useId();
  return (
    <div className="flex flex-col gap-2">
      <span className="font-medium text-sm">
        {source === "unsplash" ? (
          <Trans id="settings.sign-in.unsplash.key">Access key</Trans>
        ) : (
          <Trans id="settings.sign-in.pixabay.key">API key</Trans>
        )}
      </span>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <span className="flex items-center gap-2 text-sm">
          <KeyRoundIcon
            aria-hidden="true"
            className="size-4 text-muted-foreground"
          />
          {source === "unsplash" ? (
            <Trans id="settings.sign-in.unsplash.key.saved">
              Access key saved
            </Trans>
          ) : (
            <Trans id="settings.sign-in.pixabay.key.saved">API key saved</Trans>
          )}
        </span>
        <div className="flex gap-2">
          {replacing ? (
            <Button disabled={disabled} onClick={onKeep} variant="outline">
              <Trans id="settings.sign-in.unsplash.key.keep">
                Keep the saved key
              </Trans>
            </Button>
          ) : (
            <>
              <Button disabled={disabled} onClick={onReplace} variant="outline">
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
              >
                <Trans id="settings.sign-in.unsplash.key.remove">Remove</Trans>
              </Button>
            </>
          )}
        </div>
      </div>
      {inUse && !replacing && (
        <p id={reasonId} className="text-muted-foreground text-caption">
          <Trans id="settings.sign-in.unsplash.key.in_use">
            The current background uses this key. To remove it, switch the
            background to another source and save first.
          </Trans>
        </p>
      )}
    </div>
  );
}
