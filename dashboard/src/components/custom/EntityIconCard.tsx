import { Trans } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  type EntityIconTarget,
  removeEntityIconMutationOptions,
  uploadEntityIconMutationOptions,
} from "@/api/mutations";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { ImageUploadControl } from "@/components/custom/ImageUploadControl";

/**
 * The icon of one application or identity provider, with the controls to
 * replace and remove it.
 *
 * The four detail pages that carry an icon — an identity provider and each of
 * the three application kinds — differ only in whose icon it is, so the card is
 * written once and told which entity it belongs to. The upload and the removal
 * both go through the caller's own mutation factory, which is where the sudo
 * step-up and the cache invalidation live.
 *
 * The preview is drawn at the size the list rows use, so what the reader
 * arranges here is what a row will show.
 */

/** What the icon endpoint decodes; matches `imageutil.ValidateRaw`. */
const iconTypes = ["image/png", "image/jpeg", "image/webp", "image/gif"];

export function EntityIconCard({
  target,
  iconUrl,
  name,
}: {
  target: EntityIconTarget;
  iconUrl?: string | undefined;
  /** Used for the placeholder letter when there is no icon. */
  name: string;
}) {
  const queryClient = useQueryClient();
  const upload = useMutation(
    uploadEntityIconMutationOptions(queryClient, target),
  );
  const remove = useMutation(
    removeEntityIconMutationOptions(queryClient, target),
  );

  // Only the latest attempt's failure is worth showing: a failed removal the
  // reader already retried with an upload should not keep reporting itself.
  const failure =
    upload.submittedAt >= remove.submittedAt ? upload.error : remove.error;

  return (
    <ConsoleCard>
      <div className="flex items-center gap-6">
        <EntityAvatar
          iconUrl={iconUrl}
          className="size-16 shrink-0"
          fallback={<span className="text-lg">{name.slice(0, 1)}</span>}
        />
        <ImageUploadControl
          types={iconTypes}
          hasImage={iconUrl !== undefined}
          uploadLabel={<Trans id="entity.icon.upload">Upload an icon</Trans>}
          replaceLabel={<Trans id="entity.icon.replace">Replace icon</Trans>}
          removeLabel={<Trans id="entity.icon.remove">Remove icon</Trans>}
          hint={
            <Trans id="entity.icon.hint">
              PNG, JPEG, WebP or GIF, up to 5 MiB.
            </Trans>
          }
          isUploading={upload.isPending}
          isRemoving={remove.isPending}
          failure={failure}
          onUpload={(file) => upload.mutate(file)}
          onRemove={() => remove.mutate()}
        />
      </div>
    </ConsoleCard>
  );
}
