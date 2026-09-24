import { Avatar, Description, Label, Radio, RadioGroup } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Upload, UserRound } from "lucide-react";
import { useRef, useState } from "react";
import { client } from "@/api/client";
import { describeError } from "@/api/errors";
import { successMessage } from "@/api/success-messages";
import { notifySuccess } from "@/components/custom/AppNotifications";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

const MAX_AVATAR_BYTES = 5 * 1024 * 1024;
const ACCEPTED_TYPES = "image/png,image/jpeg,image/webp,image/gif,image/avif";

/**
 * The two checks worth making before sending a file: the server enforces both
 * limits anyway, and this only avoids a doomed upload. Anything that passes is
 * still the server's call.
 */
export function rejectAvatar(file: {
  size: number;
  type: string;
}): "too_large" | "not_image" | null {
  if (file.size > MAX_AVATAR_BYTES) return "too_large";
  if (!file.type.startsWith("image/")) return "not_image";
  return null;
}

type Session = {
  avatarUrl?: string;
  avatarPending?: boolean;
  avatarSource?: string;
  avatarSourceUrls?: Record<string, string>;
  avatarSourceLabels?: Record<string, string>;
};

/** The source the account is currently displaying, as a picker value. */
function activeSource(session: Session): string {
  return session.avatarSource ?? "none";
}

export function AvatarPanel({ current }: { current: Session }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const [failure, setFailure] = useState<unknown>(null);
  const [localError, setLocalError] = useState<string | null>(null);
  const [pending, setPending] = useState(false);
  const input = useRef<HTMLInputElement>(null);

  // Every avatar write changes `SessionView`: the source, the URL and the
  // pending flag all come from there, so the session cache is the one thing to
  // refresh. `avatarUrl` already carries a fresh query string per version, so
  // `Avatar.Image` needs no extra cache-busting.
  const refreshSession = async () => {
    await queryClient.invalidateQueries({ queryKey: ["session", "me"] });
  };

  const select = useMutation({
    retry: false,
    meta: { success: successMessage.updateAvatar },
    mutationFn: async (source: string) => {
      await client.PUT("/api/prohibitorum/me/avatar/selection", {
        body: { source },
      });
    },
    onSuccess: refreshSession,
    onError: setFailure,
  });

  const remove = useMutation({
    retry: false,
    meta: { success: successMessage.removeAvatarUpload },
    mutationFn: async () => {
      await client.DELETE("/api/prohibitorum/me/avatar");
    },
    onSuccess: refreshSession,
    onError: setFailure,
  });

  /**
   * Uploads raw bytes, not a multipart form: the server caps the body at 5 MiB
   * and re-encodes to webp, so the checks here only save a doomed round trip.
   * The chosen file stays selected on failure so the user can retry.
   */
  async function upload(file: File) {
    setFailure(null);
    setLocalError(null);
    const rejected = rejectAvatar(file);
    if (rejected === "too_large") {
      setLocalError(
        t({
          id: "profile.avatar.too_large",
          message: "Choose an image smaller than 5 MiB.",
        }),
      );
      return;
    }
    if (rejected === "not_image") {
      setLocalError(
        t({
          id: "profile.avatar.not_image",
          message: "Choose an image file.",
        }),
      );
      return;
    }
    setPending(true);
    try {
      // Through the shared client, so an error arrives as an `ApiError` with
      // the server's code rather than a bare status.
      await client.PUT("/api/prohibitorum/me/avatar", {
        body: file,
        // openapi-fetch serialises JSON by default; the endpoint wants the raw
        // bytes, and the declared content type is already a binary one.
        bodySerializer: (value) => value as BodyInit,
      });
      await refreshSession();
      // A direct request rather than a mutation, so it announces itself.
      notifySuccess(successMessage.updateAvatar);
    } catch (error) {
      setFailure(error);
    } finally {
      setPending(false);
    }
  }

  // Sources come from the session, and the account's own upload arrives as the
  // `user` entry already; the fixed entries are folded in through a set so a
  // server that reports them anyway cannot produce a duplicate option.
  const sources = Object.keys(current.avatarSourceUrls ?? {});
  const value = activeSource(current);
  const options = [...new Set(["user", ...sources, "none"])];
  const hasUpload = sources.includes("user");
  const busy = pending || select.isPending || remove.isPending;

  return (
    <ConsoleCard title={<Trans id="profile.avatar.title">Avatar</Trans>}>
      <div className="flex flex-col gap-6">
        <div className="flex items-center gap-4">
          <Avatar className="size-16 shrink-0">
            {current.avatarUrl && (
              <Avatar.Image src={current.avatarUrl} alt="" />
            )}
            <Avatar.Fallback>
              <UserRound size={28} aria-hidden="true" />
            </Avatar.Fallback>
          </Avatar>
          {current.avatarPending && (
            <p className="text-sm text-muted">
              <Trans id="profile.avatar.pending">
                Your picture from the upstream provider is still syncing. It
                will appear here once it arrives.
              </Trans>
            </p>
          )}
        </div>

        <div className="flex flex-col gap-2">
          <input
            ref={input}
            type="file"
            accept={ACCEPTED_TYPES}
            className="sr-only"
            onChange={(event) => {
              const file = event.target.files?.[0];
              // Reset so choosing the same file twice still fires a change.
              event.target.value = "";
              if (file) void upload(file);
            }}
          />
          <div className="flex flex-wrap gap-2">
            <Button
              variant="secondary"
              isDisabled={busy}
              isPending={pending}
              onPress={() => input.current?.click()}
            >
              {({ isPending }) => (
                <>
                  {!isPending && <Upload size={16} aria-hidden="true" />}
                  {hasUpload ? (
                    <Trans id="profile.avatar.replace">Replace upload</Trans>
                  ) : (
                    <Trans id="profile.avatar.upload">Upload a picture</Trans>
                  )}
                </>
              )}
            </Button>
            {hasUpload && (
              <Button
                variant="danger-soft"
                isDisabled={busy}
                isPending={remove.isPending}
                onPress={() => {
                  setFailure(null);
                  remove.mutate();
                }}
              >
                <Trans id="profile.avatar.remove">Remove upload</Trans>
              </Button>
            )}
          </div>
          <Description>
            <Trans id="profile.avatar.hint">
              PNG, JPEG, WebP, GIF or AVIF, up to 5 MiB. Larger pictures are
              scaled down.
            </Trans>
          </Description>
        </div>

        {localError && (
          <SurfaceAlert status="warning" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>{localError}</SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        {failure !== null && (
          <SurfaceAlert status="danger" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                {t(describeError(failure))}
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        <RadioGroup
          aria-label={t({
            id: "profile.avatar.source",
            message: "Picture to show",
          })}
          value={value}
          onChange={(next) => {
            setFailure(null);
            select.mutate(String(next));
          }}
          isDisabled={busy}
        >
          <Label>
            <Trans id="profile.avatar.source.label">Picture to show</Trans>
          </Label>
          {options.map((source) => (
            <Radio key={source} value={source}>
              <Radio.Content>
                <Radio.Control>
                  <Radio.Indicator />
                </Radio.Control>
                {source === "user" ? (
                  <Trans id="profile.avatar.source.user">
                    My uploaded picture
                  </Trans>
                ) : source === "none" ? (
                  <Trans id="profile.avatar.source.none">No picture</Trans>
                ) : (
                  (current.avatarSourceLabels?.[source] ?? source)
                )}
              </Radio.Content>
            </Radio>
          ))}
        </RadioGroup>
        {!hasUpload && (
          <Description>
            <Trans id="profile.avatar.no_upload">
              You have not uploaded a picture yet.
            </Trans>
          </Description>
        )}
      </div>
    </ConsoleCard>
  );
}
