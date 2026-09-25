import { Description } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { Upload } from "lucide-react";
import { type ReactNode, useRef, useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import { Button } from "@/components/custom/Button";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

/** The size cap every image endpoint on this server shares. */
export const maxImageBytes = 5 * 1024 * 1024;

/**
 * The two checks worth making before sending a file: the server enforces both
 * limits anyway, and this only avoids a doomed upload. Anything that passes is
 * still the server's call. Size is reported first, so a huge file of the wrong
 * kind still says it is too large.
 */
export function rejectImage(
  file: { size: number; type: string },
  limits: { types: readonly string[]; maxBytes: number },
): "too_large" | "wrong_type" | null {
  if (file.size > limits.maxBytes) return "too_large";
  if (!limits.types.includes(file.type)) return "wrong_type";
  return null;
}

/**
 * Choosing, replacing and removing one image: the avatar on the profile page,
 * the instance icon and the sign-in background in the settings.
 *
 * The file input is hidden behind the upload button, which reads "Replace"
 * once there is an image to replace, and the remove button appears only then.
 * The size and type are checked here before anything is sent; the message for
 * a rejected file, and for a failure the caller passes back, is drawn below
 * the buttons on the card's surface. The preview is the caller's, since an
 * avatar, an icon and a background are each shown their own way.
 */
export function ImageUploadControl({
  types,
  maxBytes = maxImageBytes,
  hasImage,
  uploadLabel,
  replaceLabel,
  removeLabel,
  hint,
  isUploading = false,
  isRemoving = false,
  isDisabled = false,
  failure = null,
  onUpload,
  onRemove,
}: {
  /** MIME types the endpoint accepts; also what the file picker offers. */
  types: readonly string[];
  maxBytes?: number;
  /** Whether there is an image of the caller's own to replace or remove. */
  hasImage: boolean;
  uploadLabel: ReactNode;
  replaceLabel: ReactNode;
  removeLabel: ReactNode;
  /** The formats and the size cap, in one line. */
  hint: ReactNode;
  isUploading?: boolean;
  isRemoving?: boolean;
  /** Another write on the same card is in flight. */
  isDisabled?: boolean;
  /** A failed upload or removal, as the caller caught it. */
  failure?: unknown;
  onUpload: (file: File) => void;
  onRemove: () => void;
}) {
  const { t } = useLingui();
  const input = useRef<HTMLInputElement>(null);
  const [rejected, setRejected] = useState<"too_large" | "wrong_type" | null>(
    null,
  );
  const busy = isDisabled || isUploading || isRemoving;
  const limitMiB = Math.round(maxBytes / (1024 * 1024));

  return (
    <div className="flex flex-col gap-2">
      <input
        ref={input}
        type="file"
        accept={types.join(",")}
        className="sr-only"
        tabIndex={-1}
        aria-hidden="true"
        onChange={(event) => {
          const file = event.target.files?.[0];
          // Reset so choosing the same file twice still fires a change.
          event.target.value = "";
          if (!file) return;
          const reason = rejectImage(file, { types, maxBytes });
          setRejected(reason);
          if (reason === null) onUpload(file);
        }}
      />
      <div className="flex flex-wrap gap-2">
        <Button
          variant="secondary"
          isDisabled={busy}
          isPending={isUploading}
          onPress={() => input.current?.click()}
        >
          {({ isPending }) => (
            <>
              {!isPending && <Upload size={16} aria-hidden="true" />}
              {hasImage ? replaceLabel : uploadLabel}
            </>
          )}
        </Button>
        {hasImage && (
          <Button
            variant="danger-soft"
            isDisabled={busy}
            isPending={isRemoving}
            onPress={() => {
              setRejected(null);
              onRemove();
            }}
          >
            {removeLabel}
          </Button>
        )}
      </div>
      <Description>{hint}</Description>

      {rejected !== null && (
        <SurfaceAlert status="warning" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              {rejected === "too_large" ? (
                <Trans id="image-upload.too_large">
                  Choose an image no larger than {limitMiB} MiB.
                </Trans>
              ) : (
                <Trans id="image-upload.wrong_type">
                  This file type is not supported. Choose one of the formats
                  listed above.
                </Trans>
              )}
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}

      {/* A dismissed identity check is the admin backing out, not a failure. */}
      {failure !== null &&
        failure !== undefined &&
        !isCancellation(failure) && (
          <SurfaceAlert status="danger" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                {t(describeError(failure))}
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}
    </div>
  );
}
