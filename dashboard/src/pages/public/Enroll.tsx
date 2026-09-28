import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type ReactNode, useEffect, useRef, useState } from "react";
import { generateTotpSecret } from "@/api/auth";
import type { EnrollmentPreview } from "@/api/enrollment";
import { ApiError, isCancellation } from "@/api/errors";
import {
  enrollPasskeyMutationOptions,
  enrollPasswordTotpMutationOptions,
} from "@/api/mutations";
import { clearSessionQueries, enrollmentQueryOptions } from "@/api/queries";
import type { EnrollmentAccountFields } from "@/api/raw-paths";
import { Button } from "@/components/custom/Button";
import { ReadOnlyField } from "@/components/custom/FormFields";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { OrSeparator } from "@/components/custom/OrSeparator";
import { ProviderButtons } from "@/components/custom/ProviderButtons";
import { PublicStep } from "@/components/custom/PublicStep";
import { applyServerError, type ServerFieldMap } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { PasskeyButton } from "@/pages/public/enroll/PasskeyButton";
import { PasswordTotpSetup } from "@/pages/public/enroll/PasswordTotpSetup";
import { Route } from "@/routes/_public.enroll.$token";

type AccountField = "username" | "displayName";

const accountFields: ServerFieldMap<AccountField> = {
  locations: { username: "username", displayName: "displayName" },
  codes: {
    username_taken: "username",
    invalid_username: "username",
    username_immutable: "username",
    invalid_display_name: "displayName",
  },
};

/** The account field a failed request belongs on, when it belongs on one. */
function accountFieldFor(error: unknown): AccountField | undefined {
  if (!(error instanceof ApiError) || error.code === undefined) return;
  if (error.code === "validation_failed") {
    const location = error.details?.location;
    return typeof location === "string" &&
      Object.hasOwn(accountFields.locations, location)
      ? accountFields.locations[location]
      : undefined;
  }
  return Object.hasOwn(accountFields.codes, error.code)
    ? accountFields.codes[error.code]
    : undefined;
}

const usernameRequired = msg({
  id: "enroll.username.required",
  message: "Enter a username.",
});
const displayNameInvalid = msg({
  id: "enroll.display_name.invalid",
  message: "Enter a display name of 1–128 characters.",
});

function enrollmentAddress(token: string, slug: string): string {
  const query = new URLSearchParams({ provider: slug });
  return `/api/prohibitorum/enrollments/${encodeURIComponent(token)}/start-federation?${query}`;
}

/**
 * Creating an account from an enrollment link, or setting up a new sign-in
 * for the account a reset names. The first step says what the link is for,
 * asks for the account's names where the reader chooses them, and offers the
 * ways to sign in the link allows: a passkey, a password and an
 * authenticator, or an upstream provider.
 *
 * The steps are the page's own state rather than routes: nothing is saved
 * until the last request, so a reload starting over is right. The first step
 * stays mounted while the password and authenticator are set, which keeps
 * its fields, and a name the server turns down there comes back to it.
 */
export function EnrollPage() {
  const { token } = Route.useParams();
  const { data: preview } = useSuspenseQuery(enrollmentQueryOptions(token));
  const { t, i18n } = useLingui();
  const { name: instance } = useInstanceBranding();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const passkey = useMutation(enrollPasskeyMutationOptions(token));
  const passwordTotp = useMutation(enrollPasswordTotpMutationOptions(token));
  const [mode, setMode] = useState<"choose" | "password-totp">("choose");
  // Kept across leaving and coming back to the password setup, so the
  // authenticator the reader already added still matches.
  const [secret] = useState(() => generateTotpSecret());
  // Bumped on every return to the first step, so its title takes focus again.
  const [returns, setReturns] = useState(0);
  const [leaving, setLeaving] = useState(false);
  // A provider button has sent the page to the provider.
  const [federating, setFederating] = useState(false);
  const focusField = useRef<AccountField>(undefined);
  const fieldsRef = useRef<HTMLDivElement>(null);

  const bound = preview.expectedUpstreamIdpSlug;
  const boundProvider = preview.providers.find(
    (provider) => provider.slug === bound,
  );
  const reset = preview.intent === "reset";
  const asksNames = !reset && boundProvider === undefined;
  const supported = window.isSecureContext && browserSupportsWebAuthn();
  const allowsPasswordTotp = preview.allowedMethods.includes("password_totp");

  const form = useAppForm({
    defaultValues: {
      username: preview.username ?? "",
      displayName:
        preview.intent === "federated_register"
          ? (preview.suggestedDisplayName ?? "")
          : "",
    },
    onSubmit: async ({ value }) => {
      // Without passkeys the password setup is the form's submit button.
      if (!supported) {
        if (allowsPasswordTotp) setMode("password-totp");
        return;
      }
      try {
        await passkey.mutateAsync(accountValues(value));
        await finish();
      } catch (error) {
        if (!isCancellation(error)) markField(error);
      }
    },
  });

  function accountValues(value: {
    username: string;
    displayName: string;
  }): EnrollmentAccountFields | undefined {
    if (!asksNames) return undefined;
    return { username: value.username, displayName: value.displayName };
  }

  /** Marks a failure on its field; anything else the toast has reported. */
  function markField(error: unknown): boolean {
    const field = accountFieldFor(error);
    if (field === undefined) return false;
    applyServerError(form, error, accountFields);
    return true;
  }

  async function finish() {
    setLeaving(true);
    await clearSessionQueries(queryClient);
    await navigate({ to: "/" });
  }

  function backToChoice(field?: AccountField) {
    focusField.current = field;
    setReturns((count) => count + 1);
    setMode("choose");
  }

  // The fields are hidden until the first step is back in view, and a hidden
  // field does not take focus.
  useEffect(() => {
    if (mode !== "choose" || focusField.current === undefined) return;
    const name = focusField.current;
    focusField.current = undefined;
    fieldsRef.current
      ?.querySelector<HTMLInputElement>(`input[name="${name}"]`)
      ?.focus();
  }, [mode]);

  async function choosePasswordTotp() {
    await form.validateAllFields("submit");
    if (!form.state.isFieldsValid) {
      fieldsRef.current
        ?.querySelector<HTMLInputElement>('input[aria-invalid="true"]')
        ?.focus();
      return;
    }
    setMode("password-totp");
  }

  const expires = new Intl.DateTimeFormat(i18n.locale, {
    dateStyle: "medium",
  }).format(new Date(preview.expiresAt));
  const accountName = reset
    ? (preview.target?.username ?? "")
    : form.state.values.username;
  const passkeyPending = passkey.isPending || leaving;
  const busy = passkeyPending || federating;
  const title = enrollTitle(preview, t);

  return (
    <>
      {mode === "password-totp" && (
        <PasswordTotpSetup
          accountName={accountName}
          secret={secret}
          onBack={() => backToChoice()}
          submit={async ({ password, secretBase32, code }) => {
            const result = await passwordTotp.mutateAsync({
              ...accountValues(form.state.values),
              password,
              secret_base32: secretBase32,
              code,
            });
            return result.recoveryCodes;
          }}
          onSubmitError={(error) => {
            const field = accountFieldFor(error);
            if (field === undefined) return false;
            markField(error);
            backToChoice(field);
            return true;
          }}
          onDone={finish}
        />
      )}
      <div hidden={mode !== "choose"}>
        <form.AppForm>
          <form.Form label={title}>
            <PublicStep
              titleKey={`choose-${returns}`}
              title={title}
              description={
                <EnrollDescription
                  preview={preview}
                  instance={instance}
                  expires={expires}
                />
              }
              actions={
                <PublicStep.Actions>
                  {boundProvider ? (
                    <ProviderButtons
                      providers={[boundProvider]}
                      href={(provider) =>
                        enrollmentAddress(token, provider.slug)
                      }
                      onLeavingChange={setFederating}
                    />
                  ) : (
                    <>
                      {/* Enter submits with the first enabled submit button,
                          so without passkeys the password setup takes it. */}
                      <PasskeyButton
                        type={supported ? "submit" : "button"}
                        supported={supported}
                        isPending={passkeyPending}
                        isDisabled={busy && !passkeyPending}
                      >
                        <Trans id="enroll.passkey">Create a passkey</Trans>
                      </PasskeyButton>
                      {allowsPasswordTotp && (
                        <Button
                          type={supported ? "button" : "submit"}
                          variant="secondary"
                          fullWidth
                          isDisabled={busy}
                          onPress={
                            supported
                              ? () => void choosePasswordTotp()
                              : undefined
                          }
                        >
                          <Trans id="enroll.use_password_totp">
                            Use a password and an authenticator instead
                          </Trans>
                        </Button>
                      )}
                      {preview.providers.length > 0 && (
                        <>
                          <OrSeparator />
                          <ProviderButtons
                            providers={preview.providers}
                            href={(provider) =>
                              enrollmentAddress(token, provider.slug)
                            }
                            isDisabled={passkeyPending}
                            onLeavingChange={setFederating}
                          />
                        </>
                      )}
                    </>
                  )}
                </PublicStep.Actions>
              }
            >
              {boundProvider && (
                <p className="text-sm">
                  <BoundInvitation name={boundProvider.displayName} />
                </p>
              )}
              {asksNames && (
                <div ref={fieldsRef} className="flex flex-col gap-4">
                  {preview.username ? (
                    <ReadOnlyField
                      label={<Trans id="enroll.username">Username</Trans>}
                      value={preview.username}
                    />
                  ) : (
                    <form.AppField
                      name="username"
                      validators={{
                        onBlur: ({ value }) =>
                          value.length > 0 ? undefined : usernameRequired,
                      }}
                    >
                      {(field) => (
                        <field.FormField
                          label={<Trans id="enroll.username">Username</Trans>}
                          autoComplete="username"
                          autoCapitalize="none"
                          spellCheck={false}
                          isDisabled={busy}
                          variant="secondary"
                        />
                      )}
                    </form.AppField>
                  )}
                  <form.AppField
                    name="displayName"
                    validators={{
                      onBlur: ({ value }) => {
                        const length = [...value].length;
                        return length > 0 && length <= 128
                          ? undefined
                          : displayNameInvalid;
                      },
                    }}
                  >
                    {(field) => (
                      <field.FormField
                        label={
                          <Trans id="enroll.display_name">Display name</Trans>
                        }
                        autoComplete="name"
                        isDisabled={busy}
                        variant="secondary"
                      />
                    )}
                  </form.AppField>
                </div>
              )}
            </PublicStep>
          </form.Form>
        </form.AppForm>
      </div>
    </>
  );
}

function BoundInvitation({ name }: { name: string }) {
  return (
    <Trans id="enroll.bound">
      This invitation needs the account to be created through {name}.
    </Trans>
  );
}

/** The page's title, which also names its form. */
function enrollTitle(
  preview: EnrollmentPreview,
  t: ReturnType<typeof useLingui>["t"],
): string {
  switch (preview.intent) {
    case "bootstrap":
      return t({
        id: "enroll.title.bootstrap",
        message: "Create the administrator account",
      });
    case "reset": {
      const displayName = preview.target?.displayName ?? "";
      return t({
        id: "enroll.title.reset",
        message: `Set up a new sign-in for ${displayName}`,
      });
    }
    default:
      return t({ id: "enroll.title.create", message: "Create your account" });
  }
}

function EnrollDescription({
  preview,
  instance,
  expires,
}: {
  preview: EnrollmentPreview;
  instance: string;
  expires: string;
}): ReactNode {
  switch (preview.intent) {
    case "bootstrap":
      return (
        <Trans id="enroll.description.bootstrap">
          This is the first account on {instance}.
        </Trans>
      );
    case "invite":
      return (
        <Trans id="enroll.description.invite">
          You've been invited to {instance}. This link works until {expires}.
        </Trans>
      );
    case "federated_register":
      return (
        <Trans id="enroll.description.federated">
          Your VRChat profile is verified. Choose your account name, then set up
          how you'll sign in.
        </Trans>
      );
    case "reset": {
      const username = preview.target?.username ?? "";
      return (
        <>
          <Trans id="enroll.description.reset">
            You can sign in with it as soon as it's set up.
          </Trans>
          <span className="block">@{username}</span>
        </>
      );
    }
  }
}
