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
import { CopyValue } from "@/components/custom/CopyValue";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

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
 * The name others see, which the account may change, and the two identifiers
 * it may not: the username it signs in with and the subject applications know
 * it by. Both identifiers are copied with a click, for quoting to an
 * application's administrator. Everything comes from the session the console
 * shell has already loaded.
 */
export function AccountPanel() {
  const { data: session } = useSuspenseQuery({
    ...sessionQueryOptions(),
    refetchOnMount: false,
  });
  if (session === null) return null;
  return (
    <Section title={<Trans id="profile.account.title">Account</Trans>}>
      <AccountForm
        displayName={session.displayName}
        username={session.username}
        subject={session.oidcSubject}
      />
    </Section>
  );
}

function AccountForm({
  displayName,
  username,
  subject,
}: {
  displayName: string;
  username: string;
  subject: string;
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
                label={
                  <Trans id="profile.display-name.label">Display name</Trans>
                }
                autoComplete="nickname"
                variant="secondary"
              />
            )}
          </form.AppField>
          <CopyValue
            label={<Trans id="console.username">Username</Trans>}
            value={username}
          />
          <CopyValue
            label={<Trans id="profile.oidc-subject">OIDC subject</Trans>}
            value={subject}
            description={
              <Trans id="profile.oidc-subject.hint">
                Applications identify you by this value. It never changes.
              </Trans>
            }
          />
          <form.SubmitButton>
            <Trans id="profile.display-name.save">Save</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}
