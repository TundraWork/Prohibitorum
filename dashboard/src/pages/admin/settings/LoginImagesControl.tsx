import { Description, Skeleton } from "@heroui/react";
import { plural } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ImagePlus, X } from "lucide-react";
import { useState } from "react";
import { ApiError } from "@/api/errors";
import {
  removeLoginImageMutationOptions,
  uploadLoginImageMutationOptions,
} from "@/api/mutations";
import { Button } from "@/components/custom/Button";
import { ImageDropTile } from "@/components/custom/ImageDropTile";
import {
  maxImageBytes,
  rejectImage,
} from "@/components/custom/ImageUploadControl";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import {
  loginImageTypes,
  maxLoginImages,
} from "@/pages/admin/settings/sign-in-page-form";

type Skip = "too_large" | "wrong_type" | "full";

/**
 * Which of the chosen files go up: the ones of an accepted type and size, and
 * of those only as many as there are free places, in the order they were
 * chosen. The rest are counted by why they were left out.
 */
export function planUploads(
  files: readonly File[],
  existing: number,
): { accepted: File[]; skipped: Record<Skip, number> } {
  const skipped: Record<Skip, number> = {
    too_large: 0,
    wrong_type: 0,
    full: 0,
  };
  const accepted: File[] = [];
  let room = Math.max(0, maxLoginImages - existing);
  for (const file of files) {
    const rejection = rejectImage(file, {
      types: loginImageTypes,
      maxBytes: maxImageBytes,
    });
    if (rejection !== null) {
      skipped[rejection] += 1;
    } else if (room === 0) {
      skipped.full += 1;
    } else {
      accepted.push(file);
      room -= 1;
    }
  }
  return { accepted, skipped };
}

/**
 * The uploaded sign-in images, in upload order, with an add tile at the end
 * until there are ten. Adding and removing are immediate — they do not wait
 * for the settings' Save — and each reports its own success; a failed request
 * is the error toast's. Several files go up one at a time, each holding a
 * placeholder tile until it lands.
 */
export function LoginImagesControl({
  images,
}: {
  images: { id: number; url: string }[];
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const upload = useMutation(uploadLoginImageMutationOptions(queryClient));
  const [queued, setQueued] = useState(0);
  const [skipped, setSkipped] = useState<Record<Skip, number> | null>(null);

  const count = images.length;
  const full = count + queued >= maxLoginImages;

  const add = async (files: readonly File[]) => {
    const plan = planUploads(files, count + queued);
    const left = Object.values(plan.skipped).some((n) => n > 0);
    setSkipped(left ? plan.skipped : null);
    setQueued((n) => n + plan.accepted.length);
    for (const file of plan.accepted) {
      try {
        await upload.mutateAsync(file);
      } catch (error) {
        // Reported by the toast. A full list will not take the rest either.
        if (error instanceof ApiError && error.code === "login_images_full") {
          setQueued(0);
          return;
        }
      } finally {
        setQueued((n) => Math.max(0, n - 1));
      }
    }
  };

  const empty = count === 0 && queued === 0;
  const addTile = (
    <ImageDropTile
      ariaLabel={t({
        id: "settings.sign-in.images.drop",
        message: "Drop images here, or choose files",
      })}
      label={
        empty ? (
          <Trans id="settings.sign-in.images.drop">
            Drop images here, or choose files
          </Trans>
        ) : (
          <Trans id="settings.sign-in.images.add">Add images</Trans>
        )
      }
      icon={ImagePlus}
      acceptedFileTypes={loginImageTypes}
      allowsMultiple
      onFiles={(files) => void add(files)}
      className={empty ? "aspect-[3/1]" : "aspect-video"}
    />
  );

  return (
    <div className="flex flex-col gap-2">
      <ul
        className="grid grid-cols-3 gap-2"
        aria-label={t({ id: "settings.sign-in.images", message: "Images" })}
      >
        {images.map((image, index) => (
          <ImageTile key={image.id} image={image} position={index + 1} />
        ))}
        {Array.from({ length: queued }, (_, index) => (
          // biome-ignore lint/suspicious/noArrayIndexKey: placeholders have no identity.
          <li key={`queued-${index}`} className="aspect-video">
            <Skeleton className="size-full rounded-[0.375rem]" />
          </li>
        ))}
        {!full && <li className={empty ? "col-span-full" : ""}>{addTile}</li>}
      </ul>
      <Description>
        {count >= maxLoginImages ? (
          <Trans id="settings.sign-in.images.full">
            10 of 10 · remove one to add another
          </Trans>
        ) : (
          <Trans id="settings.sign-in.images.hint">
            PNG, JPEG or WebP, up to 5 MiB each · {count} of 10
          </Trans>
        )}
      </Description>
      {skipped !== null && <SkippedNotice skipped={skipped} />}
    </div>
  );
}

function SkippedNotice({ skipped }: { skipped: Record<Skip, number> }) {
  const { t } = useLingui();
  const describe: Record<Skip, (count: number) => string> = {
    too_large: (count) =>
      t({
        id: "settings.sign-in.images.skipped.too_large",
        message: plural(count, {
          one: "# file wasn't uploaded because it is larger than 5 MiB.",
          other: "# files weren't uploaded because they are larger than 5 MiB.",
        }),
      }),
    wrong_type: (count) =>
      t({
        id: "settings.sign-in.images.skipped.wrong_type",
        message: plural(count, {
          one: "# file wasn't uploaded because it isn't PNG, JPEG or WebP.",
          other:
            "# files weren't uploaded because they aren't PNG, JPEG or WebP.",
        }),
      }),
    full: (count) =>
      t({
        id: "settings.sign-in.images.skipped.full",
        message: plural(count, {
          one: "# file wasn't uploaded because the 10-image limit was reached.",
          other:
            "# files weren't uploaded because the 10-image limit was reached.",
        }),
      }),
  };
  const lines = (Object.keys(describe) as Skip[])
    .filter((key) => skipped[key] > 0)
    .map((key) => ({ key, text: describe[key](skipped[key]) }));
  const [first, ...rest] = lines;
  if (first === undefined) return null;
  return (
    <SurfaceAlert status="warning">
      <SurfaceAlert.Indicator />
      <SurfaceAlert.Content>
        <SurfaceAlert.Title>{first.text}</SurfaceAlert.Title>
        {rest.map((line) => (
          <SurfaceAlert.Description key={line.key}>
            {line.text}
          </SurfaceAlert.Description>
        ))}
      </SurfaceAlert.Content>
    </SurfaceAlert>
  );
}

function ImageTile({
  image,
  position,
}: {
  image: { id: number; url: string };
  position: number;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const remove = useMutation(removeLoginImageMutationOptions(queryClient));
  return (
    <li className="relative aspect-video overflow-hidden rounded-[0.375rem] bg-default">
      <img
        src={image.url}
        alt=""
        className={`size-full object-cover transition-opacity ${remove.isPending ? "opacity-50" : ""}`}
      />
      <Button
        isIconOnly
        size="sm"
        variant="tertiary"
        isPending={remove.isPending}
        aria-label={t({
          id: "settings.sign-in.images.remove",
          message: `Remove image ${position}`,
        })}
        onPress={() => remove.mutate(image.id)}
        className="absolute top-1.5 right-1.5 size-7 min-w-7 rounded-full bg-surface/85 shadow-surface backdrop-blur-sm"
      >
        <X size={14} aria-hidden="true" />
      </Button>
    </li>
  );
}
