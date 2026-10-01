import { Avatar, Badge, Description, RadioGroup, Spinner } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { Check, Upload, UserRound } from "lucide-react";
import { useState } from "react";
import {
  removeAvatarUploadMutationOptions,
  selectAvatarMutationOptions,
  uploadAvatarMutationOptions,
} from "@/api/mutations";
import { myAvatarQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { ImageDropTile } from "@/components/custom/ImageDropTile";
import {
  ImageRejectedAlert,
  maxImageBytes,
  rejectImage,
} from "@/components/custom/ImageUploadControl";
import { AsyncSection, Section } from "@/components/custom/Section";

/** What `PUT /me/avatar` decodes; the file picker offers the same list. */
export const avatarTypes = [
  "image/png",
  "image/jpeg",
  "image/webp",
  "image/gif",
  "image/avif",
] as const;

/**
 * The avatar section: every picture the account can show, side by side, with
 * the one in use selected. The section keeps its heading while the pictures
 * load, and a failed read stays inside it.
 */
export function AvatarPanel() {
  return (
    <AsyncSection
      resetKey="avatar"
      title={<Trans id="profile.avatar.title">Avatar</Trans>}
    >
      <AvatarGallery />
    </AsyncSection>
  );
}

/**
 * One tile per picture, then "No picture", then the tile that uploads. The
 * selected tile is the avatar in use, so there is no separate preview to say
 * the same thing twice.
 *
 * An upload is only stored: the server shows it at once only for an account
 * that has never chosen a picture, and otherwise it waits in the first tile to
 * be chosen. One write runs at a time, and a failed one is the error toast's.
 */
function AvatarGallery() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const { data: avatar } = useSuspenseQuery(myAvatarQueryOptions());
  const select = useMutation(selectAvatarMutationOptions(queryClient));
  const upload = useMutation(uploadAvatarMutationOptions(queryClient));
  const remove = useMutation(removeAvatarUploadMutationOptions(queryClient));
  const [rejected, setRejected] = useState<"too_large" | "wrong_type" | null>(
    null,
  );
  const [confirmingRemove, setConfirmingRemove] = useState(false);

  const sources = avatar.sources ?? [];
  const hasUpload = sources.some((entry) => entry.source === "user");
  const writing = upload.isPending || remove.isPending;
  const busy = writing || select.isPending;
  // The tile just pressed shows as chosen until the server's answer lands.
  const value = select.isPending ? select.variables : avatar.activeSource;

  const choose = (files: File[]) => {
    const file = files[0];
    if (file === undefined) return;
    const reason = rejectImage(file, {
      types: avatarTypes,
      maxBytes: maxImageBytes,
    });
    setRejected(reason);
    if (reason === null) upload.mutate(file);
  };

  const tile = (source: string, url: string | undefined, label: string) => (
    <ChoiceTile
      key={source}
      value={source}
      layout="media"
      title={label}
      label={label}
      media={
        <Preview
          url={url}
          state={
            select.isPending && select.variables === source
              ? "pending"
              : value === source
                ? "selected"
                : "idle"
          }
        />
      }
    />
  );

  return (
    <Section title={<Trans id="profile.avatar.title">Avatar</Trans>}>
      <ConsoleCard wide contentClassName="flex flex-col gap-3">
        <div className="grid grid-cols-[repeat(auto-fill,minmax(6rem,1fr))] gap-2">
          {/* The group's own box steps aside so its tiles and the upload
              tile share one grid; the upload is not one of the choices. */}
          <RadioGroup
            aria-label={t({
              id: "profile.avatar.source",
              message: "Picture to show",
            })}
            variant="secondary"
            className="contents"
            value={value}
            // Read-only rather than disabled while a choice is saved, so the
            // tile keeps keyboard focus.
            isReadOnly={select.isPending}
            isDisabled={writing}
            onChange={(next) => {
              setRejected(null);
              select.mutate(next);
            }}
          >
            {sources.map((entry) =>
              tile(
                entry.source,
                entry.url,
                entry.source === "user"
                  ? t({
                      id: "profile.avatar.source.user",
                      message: "Uploaded picture",
                    })
                  : (entry.label ?? entry.source),
              ),
            )}
            {tile(
              "none",
              undefined,
              t({ id: "profile.avatar.source.none", message: "No picture" }),
            )}
          </RadioGroup>
          <ImageDropTile
            ariaLabel={t({
              id: "profile.avatar.drop",
              message: "Drop a picture here, or choose a file",
            })}
            label={
              hasUpload ? (
                <Trans id="profile.avatar.replace">Replace</Trans>
              ) : (
                <Trans id="profile.avatar.upload">Upload</Trans>
              )
            }
            icon={Upload}
            acceptedFileTypes={avatarTypes}
            isPending={upload.isPending}
            isDisabled={select.isPending || remove.isPending}
            onFiles={choose}
          />
        </div>
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
          <Description>
            <Trans id="profile.avatar.hint">
              PNG, JPEG, WebP, GIF or AVIF, up to 5 MiB.
            </Trans>
          </Description>
          {hasUpload && (
            <Button
              variant="danger-soft"
              size="sm"
              isDisabled={busy}
              onPress={() => {
                setRejected(null);
                setConfirmingRemove(true);
              }}
            >
              <Trans id="profile.avatar.remove">Remove uploaded picture</Trans>
            </Button>
          )}
        </div>
        {rejected !== null && <ImageRejectedAlert reason={rejected} />}
      </ConsoleCard>
      <ConfirmDialog
        isOpen={confirmingRemove}
        onOpenChange={setConfirmingRemove}
        status="danger"
        title={
          <Trans id="profile.avatar.remove.title">
            Remove the uploaded picture?
          </Trans>
        }
        body={
          <Trans id="profile.avatar.remove.description">
            This can't be undone.
          </Trans>
        }
        confirmLabel={<Trans id="profile.avatar.remove.confirm">Remove</Trans>}
        isPending={remove.isPending}
        onConfirm={() =>
          remove.mutate(undefined, {
            onSettled: () => setConfirmingRemove(false),
          })
        }
      />
    </Section>
  );
}

/**
 * A tile's picture, the shape the sidebar draws the account in, so the tile
 * shows what the sidebar will. The chosen one carries a check, or a spinner
 * while it is being saved.
 */
function Preview({
  url,
  state,
}: {
  url: string | undefined;
  state: "idle" | "selected" | "pending";
}) {
  return (
    <Badge.Anchor>
      <Avatar className="size-14 rounded-field">
        {url && <Avatar.Image src={url} alt="" />}
        <Avatar.Fallback className="rounded-field">
          <UserRound size={24} aria-hidden="true" />
        </Avatar.Fallback>
      </Avatar>
      {state === "selected" && (
        <Badge color="accent" size="sm" placement="bottom-right">
          <Check className="size-2.5" strokeWidth={3} aria-hidden="true" />
        </Badge>
      )}
      {state === "pending" && (
        <Badge size="sm" placement="bottom-right">
          <Spinner
            size="sm"
            color="current"
            className="size-3"
            aria-hidden="true"
          />
        </Badge>
      )}
    </Badge.Anchor>
  );
}
