import { Card, Description, Tabs } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { updateProfileMutationOptions } from "@/api/mutations";
import { sessionQueryOptions } from "@/api/queries";

import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import type { ProfileTab } from "@/pages/console/tabs";
import { AvatarPanel } from "@/pages/profile/AvatarPanel";
import { Route } from "@/routes/_protected.profile";

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
 * The account's own page. Display name and avatar are two tabs rather than two
 * stacked cards, and the selected tab lives in the URL so a shared link or a
 * reload lands where the sender was.
 */
export function Profile() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const { tab } = Route.useSearch();
  const { data: session } = useSuspenseQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });

  if (session === null) return null;

  return (
    <>
      <Tabs
        selectedKey={tab}
        onSelectionChange={(key) => {
          // replace, so browsing tabs never buries the page the user came from.
          void navigate({
            to: "/profile",
            search: { tab: key as ProfileTab },
            replace: true,
          });
        }}
      >
        <Tabs.ListContainer className="ml-2 w-fit max-w-full">
          <Tabs.List
            className="grid grid-flow-col auto-cols-fr"
            aria-label={t({ id: "profile.tabs", message: "Profile" })}
          >
            <Tabs.Tab className="whitespace-nowrap" id="display-name">
              <Trans id="profile.tab.display-name">Display name</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
            <Tabs.Tab className="whitespace-nowrap" id="avatar">
              <Trans id="profile.tab.avatar">Avatar</Trans>
              <Tabs.Indicator />
            </Tabs.Tab>
          </Tabs.List>
        </Tabs.ListContainer>
        <Tabs.Panel id="display-name">
          <DisplayNameCard
            displayName={session.displayName}
            username={session.username}
          />
        </Tabs.Panel>
        <Tabs.Panel id="avatar">
          <AvatarPanel current={session} />
        </Tabs.Panel>
      </Tabs>
    </>
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
  const [saved, setSaved] = useState(false);
  const update = useMutation(updateProfileMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: { displayName },
    onSubmit: async ({ value }) => {
      setSaved(false);
      try {
        await update.mutateAsync({ displayName: value.displayName });
        setSaved(true);
      } catch (error) {
        applyServerError(form, error, { locations: {}, codes: {} });
      }
    },
  });

  return (
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>
          <Trans id="profile.display-name.title">Display name</Trans>
        </Card.Title>
      </Card.Header>
      <Card.Content>
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
            {saved && (
              <p role="status" className="text-sm">
                <Trans id="profile.display-name.saved">
                  Your display name is updated.
                </Trans>
              </p>
            )}
            <form.SubmitButton>
              <Trans id="profile.display-name.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </Card.Content>
    </Card>
  );
}
