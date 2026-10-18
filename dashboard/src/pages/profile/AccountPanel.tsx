import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { IdCard } from "lucide-react";
import { updateProfileMutationOptions } from "@/api/mutations";
import { sessionQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { CopyValue } from "@/components/custom/CopyValue";
import { Section } from "@/components/custom/Section";
import { SettingRow } from "@/components/custom/SettingRow";
import { Separator } from "@/components/ui/separator";
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
    <AccountForm
      displayName={session.displayName}
      username={session.username}
      subject={session.oidcSubject}
    />
  );
}

/**
 * The section sits inside the form so its footer's Save is a real submit
 * button. Save and Discard appear only while the name differs from the saved one.
 */
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
        const saved = await update.mutateAsync({
          displayName: value.displayName,
        });
        // The saved name (the server trims it) becomes the baseline to compare with.
        form.reset({ displayName: saved.displayName });
      } catch (error) {
        applyServerError(form, error, { locations: {}, codes: {} });
      }
    },
  });
  const dirty = useStore(form.store, (state) => !state.isDefaultValue);

  return (
    <form.AppForm>
      <form.Form
        label={t({
          id: "profile.display-name.form",
          message: "Display name",
        })}
      >
        <Section
          icon={<IdCard />}
          title={<Trans id="profile.account.title">Account</Trans>}
          description={
            <Trans id="profile.account.description">
              Your name is shown to others; the identifiers below are fixed.
            </Trans>
          }
          dirty={dirty}
          footer={
            dirty ? (
              <div className="flex w-full items-center justify-end gap-2">
                <Button
                  variant="ghost"
                  onClick={() => form.reset()}
                  disabled={form.state.isSubmitting}
                >
                  <Trans id="profile.display-name.discard">Discard</Trans>
                </Button>
                <form.SubmitButton>
                  <Trans id="profile.display-name.save">Save</Trans>
                </form.SubmitButton>
              </div>
            ) : undefined
          }
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
          <Separator />
          <div className="flex flex-col divide-y">
            <SettingRow
              title={<Trans id="console.username">Username</Trans>}
              description={
                <Trans id="profile.username.hint">
                  What you type to sign in.
                </Trans>
              }
            >
              <div className="w-full sm:w-80">
                <CopyValue
                  label={
                    <span className="sr-only">
                      <Trans id="console.username">Username</Trans>
                    </span>
                  }
                  value={username}
                />
              </div>
            </SettingRow>
            <SettingRow
              title={<Trans id="profile.oidc-subject">OIDC subject</Trans>}
              description={
                <Trans id="profile.oidc-subject.hint">
                  Applications identify you by this value. It never changes.
                </Trans>
              }
            >
              <div className="w-full sm:w-80">
                <CopyValue
                  label={
                    <span className="sr-only">
                      <Trans id="profile.oidc-subject">OIDC subject</Trans>
                    </span>
                  }
                  value={subject}
                />
              </div>
            </SettingRow>
          </div>
        </Section>
      </form.Form>
    </form.AppForm>
  );
}
