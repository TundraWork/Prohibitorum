import { Description } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { updateProfileMutationOptions } from "@/api/mutations";
import { sessionQueryOptions } from "@/api/queries";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { AvatarPanel } from "@/pages/profile/AvatarPanel";

/** Mirrors `account.ValidateNickname`: at most 60 runes, no control characters. */
function nicknameError(value: string): "too_long" | "control" | null {
  const trimmed = value.trim();
  if (trimmed === "") return null;
  if ([...trimmed].length > 60) return "too_long";
  for (const char of trimmed) {
    const code = char.codePointAt(0) ?? 0;
    if (code < 0x20 || code === 0x7f) return "control";
  }
  return null;
}

const tooLong = msg({
  id: "profile.display-name.too_long",
  message: "Use 60 characters or fewer.",
});
const hasControl = msg({
  id: "profile.display-name.control",
  message: "Remove control characters from the name.",
});

/**
 * The account's own page: the display name and the avatar, as two sections on
 * one page rather than two tabs.
 *
 * Both are short and a reader who came to change one often wants the other, so
 * there is no strip to work through. The session read belongs to the page: both
 * sections draw from it, so it is awaited once here and passed down.
 */
export function Profile() {
  const { data: session } = useSuspenseQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });

  if (session === null) return null;

  return (
    <div className="flex flex-col gap-8">
      <Section
        title={<Trans id="profile.display-name.title">Display name</Trans>}
      >
        <DisplayNameCard
          displayName={session.displayName}
          username={session.username}
        />
      </Section>
      <Section title={<Trans id="profile.avatar.title">Avatar</Trans>}>
        <AvatarPanel current={session} />
      </Section>
    </div>
  );
}

function DisplayNameCard({
  displayName,
  username,
}: {
  displayName: string;
  username: string;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateProfileMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: { displayName },
    onSubmit: async ({ value }) => {
      try {
        await update.mutateAsync({ displayName: value.displayName });
      } catch (error) {
        applyServerError(form, error, { locations: {}, codes: {} });
      }
    },
  });

  return (
    <ConsoleCard>
      <form.AppForm>
        <form.Form
          label={t({
            id: "profile.display-name.form",
            message: "Display name",
          })}
        >
          <form.FormError />
          <form.AppField
            name="displayName"
            validators={{
              onChange: ({ value }) => {
                const kind = nicknameError(value);
                if (kind === "too_long") return tooLong;
                if (kind === "control") return hasControl;
                return undefined;
              },
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="profile.display-name.label">Name</Trans>}
                autoComplete="nickname"
                variant="secondary"
              />
            )}
          </form.AppField>
          <div className="flex flex-col gap-1">
            <span className="text-sm text-muted">
              <Trans id="console.username">Username</Trans>
            </span>
            <span className="wrap-anywhere">{username}</span>
            <Description>
              <Trans id="profile.username.note">
                Your username identifies you at sign-in and cannot be changed.
              </Trans>
            </Description>
          </div>
          <form.SubmitButton>
            <Trans id="profile.display-name.save">Save</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}
