import { Alert, Button, Card, Checkbox, Spinner } from "@heroui/react";
import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useLocation } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import {
  buildTotpUri,
  cancelPasskeyAuthentication,
  generateTotpSecret,
  isValidLoginPassword,
  isValidRecoveryCode,
  isValidTotpCode,
  parseReturnTo,
  validateRedirect,
} from "@/api/auth";
import { describeError, isCancellation } from "@/api/errors";
import {
  passkeyMutationOptions,
  passwordMutationOptions,
  recoveryMutationOptions,
  totpMutationOptions,
} from "@/api/mutations";
import {
  authStatusQueryOptions,
  clearSessionQueries,
  publicConfigQueryOptions,
} from "@/api/queries";
import type {
  PublicConfig,
  RecoveryRequest,
  RecoveryResult,
} from "@/api/raw-paths";
import { FormMessages } from "@/components/custom/FormMessages";
import { OtpField } from "@/components/custom/OtpField";
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { TotpSetup } from "@/components/custom/TotpSetup";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

const noFields = { locations: {}, codes: {} };
const passwordInvalid = msg({
  id: "login.password.invalid",
  message: "Enter a password of no more than 1024 UTF-8 bytes.",
});
const usernameInvalid = msg({
  id: "login.username.required",
  message: "Enter your username.",
});
const codeInvalid = msg({
  id: "login.totp.invalid",
  message: "Enter a code with the configured number of digits, using only 0–9.",
});
const recoveryInvalid = msg({
  id: "login.recovery.invalid",
  message:
    "Enter a recovery code in XXXX-XXXX-XXXX-XXXX format, using uppercase A–Z and digits 2–7.",
});

type FlowControl = {
  busy: boolean;
  acquire: () => boolean;
  release: () => void;
  isActive: () => boolean;
};
type Step = "password" | "totp" | "recovery";
type Failure = {
  message: MessageDescriptor;
  secondStep?: boolean;
  reset?: boolean;
};

function PasswordForm({
  control,
  username,
  onSuccess,
  onFailure,
}: {
  control: FlowControl;
  username: string;
  onSuccess: (username: string, token: string) => void;
  onFailure: (error: unknown) => void;
}) {
  const { t } = useLingui();
  const mutation = useMutation(passwordMutationOptions());
  const form = useAppForm({
    defaultValues: { username, password: "" },
    onSubmit: async ({ value, formApi }) => {
      if (!control.acquire()) return;
      try {
        const result = await mutation.mutateAsync(value);
        formApi.setFieldValue("password", "");
        if (control.isActive())
          onSuccess(value.username, result.partial_session_token);
      } catch (error) {
        if (control.isActive() && !isCancellation(error)) onFailure(error);
      } finally {
        mutation.reset();
        control.release();
      }
    },
  });
  const resetMutation = mutation.reset;
  useEffect(
    () => () => {
      resetMutation();
    },
    [resetMutation],
  );
  return (
    <form.AppForm>
      <fieldset
        disabled={control.busy && !mutation.isPending}
        className="min-w-0"
      >
        <form.Form
          label={t({
            id: "login.password.form",
            message: "Sign in with a password",
          })}
        >
          <form.AppField
            name="username"
            validators={{
              onChange: ({ value }) =>
                value.length ? undefined : usernameInvalid,
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="login.username">Username</Trans>}
                autoComplete="username"
                autoCapitalize="none"
                spellCheck={false}
                isDisabled={control.busy}
              />
            )}
          </form.AppField>
          <form.AppField
            name="password"
            validators={{
              onChange: ({ value }) =>
                isValidLoginPassword(value) ? undefined : passwordInvalid,
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="login.password">Password</Trans>}
                type="password"
                autoComplete="current-password"
                isDisabled={control.busy}
              />
            )}
          </form.AppField>
          <form.SubmitButton fullWidth>
            <Trans id="login.password.next">Continue with password</Trans>
          </form.SubmitButton>
        </form.Form>
      </fieldset>
    </form.AppForm>
  );
}

function FactorForm({
  control,
  mode,
  config,
  username,
  returnTo,
  takeToken,
  onFailure,
  onSuccess,
  onSwitch,
}: {
  control: FlowControl;
  mode: "totp" | "recovery";
  config: PublicConfig;
  username: string;
  returnTo?: string;
  takeToken: () => string | undefined;
  onFailure: (error: unknown, reset: boolean) => void;
  onSuccess: (redirect: string, codes?: string[]) => Promise<void>;
  onSwitch: () => void;
}) {
  const { t } = useLingui();
  const totp = useMutation(totpMutationOptions(returnTo));
  const recovery = useMutation(recoveryMutationOptions(returnTo));
  const [setup, setSetup] = useState<{ secret: string; uri: string }>();
  const form = useAppForm({
    defaultValues: { code: "", totpCode: "" },
    onSubmit: async ({ value, formApi }) => {
      if (!control.acquire()) return;
      const partialToken = takeToken();
      if (!partialToken) {
        control.release();
        return;
      }
      const resetting = setup !== undefined;
      try {
        const result: RecoveryResult =
          mode === "totp"
            ? await totp.mutateAsync({
                partial_session_token: partialToken,
                code: value.code,
              })
            : await recovery.mutateAsync(
                setup
                  ? ({
                      partial_session_token: partialToken,
                      code: value.code,
                      reset_authenticator: true,
                      totp_secret_base32: setup.secret,
                      totp_code: value.totpCode,
                    } satisfies RecoveryRequest)
                  : ({
                      partial_session_token: partialToken,
                      code: value.code,
                      reset_authenticator: false,
                    } satisfies RecoveryRequest),
              );
        formApi.reset();
        setSetup(undefined);
        if (control.isActive())
          await onSuccess(result.redirect, result.recovery_codes);
      } catch (error) {
        formApi.reset();
        setSetup(undefined);
        if (control.isActive()) onFailure(error, resetting);
      } finally {
        totp.reset();
        recovery.reset();
        control.release();
      }
    },
  });
  const resetTotp = totp.reset;
  const resetRecovery = recovery.reset;
  useEffect(
    () => () => {
      resetTotp();
      resetRecovery();
    },
    [resetTotp, resetRecovery],
  );

  return (
    <form.AppForm>
      <form.Form
        label={
          mode === "totp"
            ? t({ id: "login.totp.form", message: "Verify your authenticator" })
            : t({
                id: "login.recovery.form",
                message: "Sign in with a recovery code",
              })
        }
      >
        <form.FormError />
        <form.AppField
          name="code"
          validators={{
            onChange: ({ value }) =>
              mode === "totp"
                ? isValidTotpCode(value, config.totp.digits)
                  ? undefined
                  : codeInvalid
                : isValidRecoveryCode(value)
                  ? undefined
                  : recoveryInvalid,
          }}
        >
          {(field) =>
            mode === "totp" ? (
              <OtpField
                digits={config.totp.digits}
                label={<Trans id="login.totp.code">Authenticator code</Trans>}
                description={
                  <Trans id="login.totp.digits">
                    Enter the {config.totp.digits}-digit code from your
                    authenticator.
                  </Trans>
                }
              />
            ) : (
              <field.FormField
                label={<Trans id="login.recovery.code">Recovery code</Trans>}
                inputMode="text"
                autoComplete="off"
                autoCapitalize="none"
                spellCheck={false}
              />
            )
          }
        </form.AppField>
        {mode === "recovery" && (
          <>
            <Checkbox
              isSelected={setup !== undefined}
              isDisabled={control.busy}
              onChange={(selected) => {
                if (control.busy) return;
                form.resetField("totpCode");
                if (!selected) {
                  setSetup(undefined);
                  return;
                }
                try {
                  const secret = generateTotpSecret();
                  setSetup({
                    secret,
                    uri: buildTotpUri(secret, username, config.totp),
                  });
                } catch (error) {
                  applyServerError(form, error, noFields);
                }
              }}
            >
              <Checkbox.Content>
                <Checkbox.Control>
                  <Checkbox.Indicator />
                </Checkbox.Control>
                <Trans id="login.reset.toggle">Reset my authenticator</Trans>
              </Checkbox.Content>
            </Checkbox>
            {setup && (
              <>
                <TotpSetup secret={setup.secret} uri={setup.uri} />
                <form.AppField
                  name="totpCode"
                  validators={{
                    onChange: ({ value }) =>
                      isValidTotpCode(value, config.totp.digits)
                        ? undefined
                        : codeInvalid,
                  }}
                >
                  {(field) => (
                    <field.FormField
                      label={
                        <Trans id="login.reset.code">
                          New authenticator code
                        </Trans>
                      }
                      inputMode="numeric"
                      autoComplete="one-time-code"
                    />
                  )}
                </form.AppField>
              </>
            )}
          </>
        )}
        <div className="flex flex-col gap-2 [&_button]:h-auto [&_button]:min-h-10 [&_button]:whitespace-normal [&_button]:py-2 md:[&_button]:min-h-9">
          <form.SubmitButton fullWidth>
            <Trans id="login.verify">Sign in</Trans>
          </form.SubmitButton>
          {mode === "totp" && (
            <Button
              type="button"
              variant="ghost"
              fullWidth
              isDisabled={control.busy}
              onPress={onSwitch}
            >
              <Trans id="login.use_recovery">Use a recovery code</Trans>
            </Button>
          )}
        </div>
      </form.Form>
    </form.AppForm>
  );
}

function LoginFlow({
  config,
  returnTo,
}: {
  config: PublicConfig;
  returnTo?: string;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const [step, setStep] = useState<Step>("password");
  const [username, setUsername] = useState("");
  const [busy, setBusy] = useState(false);
  const [finishing, setFinishing] = useState(false);
  const [failure, setFailure] = useState<Failure>();
  const [savedCodes, setSavedCodes] = useState<{
    redirect: string;
    codes: string[];
  }>();
  const token = useRef<string | undefined>(undefined);
  const locked = useRef(false);
  const active = useRef(false);
  const focusHeading = useCallback((node: HTMLHeadingElement | null) => {
    node?.focus();
  }, []);
  const passkey = useMutation(passkeyMutationOptions(returnTo));
  const resetPasskey = passkey.reset;
  const supported = window.isSecureContext && browserSupportsWebAuthn();
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
      token.current = undefined;
      cancelPasskeyAuthentication();
      resetPasskey();
    };
  }, [resetPasskey]);

  const control: FlowControl = {
    busy: busy || finishing,
    acquire: () => {
      if (locked.current || finishing || !active.current) return false;
      locked.current = true;
      setBusy(true);
      return true;
    },
    release: () => {
      locked.current = false;
      if (active.current) setBusy(false);
    },
    isActive: () => active.current,
  };
  async function finish(redirect: string) {
    try {
      const destination = validateRedirect(redirect, window.location.origin);
      setFinishing(true);
      await clearSessionQueries(queryClient);
      if (!active.current) return;
      token.current = undefined;
      setSavedCodes(undefined);
      window.location.assign(destination);
    } catch (error) {
      setFinishing(false);
      setFailure({ message: describeError(error) });
      throw error;
    }
  }
  function back() {
    if (control.busy) return;
    if (step === "recovery") {
      setStep("totp");
      return;
    }
    token.current = undefined;
    setStep("password");
  }

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center gap-2">
        {step !== "password" && !savedCodes && (
          <Button
            type="button"
            variant="ghost"
            size="sm"
            isIconOnly
            className="shrink-0"
            aria-label={
              step === "recovery"
                ? t({
                    id: "login.use_totp",
                    message: "Use an authenticator code",
                  })
                : t({ id: "login.back", message: "Back to password" })
            }
            isDisabled={control.busy}
            onPress={back}
          >
            <ArrowLeft aria-hidden="true" />
          </Button>
        )}
        <h1
          key={`${step}-${Boolean(savedCodes)}-${Boolean(failure)}`}
          ref={focusHeading}
          tabIndex={-1}
          className="min-w-0 text-xl font-semibold wrap-anywhere"
        >
          {savedCodes ? (
            <Trans id="login.complete">Authenticator reset complete</Trans>
          ) : step === "password" ? (
            <Trans id="login.title">Sign in</Trans>
          ) : (
            <Trans id="login.factor.account">Signing in as {username}</Trans>
          )}
        </h1>
      </div>
      {failure && (
        <Alert status="danger" role="alert">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>
              <FormMessages errors={[failure.message]} />
            </Alert.Title>
            {failure.secondStep && (
              <Alert.Description>
                {failure.reset ? (
                  <Trans id="login.reset.uncertain">
                    The reset may already have completed. Try your new
                    authenticator or a passkey. Enter your password again before
                    another code attempt; your old codes may no longer work.
                  </Trans>
                ) : (
                  <Trans id="login.factor.restart">
                    Enter your password again before trying another code. This
                    verification attempt cannot be reused.
                  </Trans>
                )}
              </Alert.Description>
            )}
          </Alert.Content>
        </Alert>
      )}
      {savedCodes ? (
        <RecoveryCodes
          codes={savedCodes.codes}
          onContinue={() => finish(savedCodes.redirect)}
        />
      ) : finishing ? (
        <Spinner />
      ) : (
        <>
          {step === "password" ? (
            <PasswordForm
              control={control}
              username={username}
              onFailure={(error) =>
                setFailure({ message: describeError(error) })
              }
              onSuccess={(name, partialToken) => {
                setUsername(name);
                token.current = partialToken;
                setFailure(undefined);
                setStep("totp");
              }}
            />
          ) : (
            <FactorForm
              key={step}
              control={control}
              mode={step}
              config={config}
              username={username}
              returnTo={returnTo}
              takeToken={() => {
                const current = token.current;
                token.current = undefined;
                return current;
              }}
              onFailure={(error, reset) => {
                setFailure({
                  message: describeError(error),
                  secondStep: true,
                  reset,
                });
                setStep("password");
              }}
              onSuccess={async (redirect, codes) => {
                setFailure(undefined);
                if (codes) {
                  setSavedCodes({ redirect, codes });
                  await clearSessionQueries(queryClient);
                } else await finish(redirect);
              }}
              onSwitch={() => {
                if (!control.busy)
                  setStep(step === "totp" ? "recovery" : "totp");
              }}
            />
          )}
          {step === "password" && (
            <>
              <Button
                variant="secondary"
                fullWidth
                isPending={passkey.isPending}
                isDisabled={!supported || (control.busy && !passkey.isPending)}
                onPress={() => {
                  if (!control.acquire()) return;
                  void (async () => {
                    try {
                      const result = await passkey.mutateAsync();
                      if (active.current) await finish(result.redirect);
                    } catch (error) {
                      if (active.current && !isCancellation(error))
                        setFailure({ message: describeError(error) });
                    } finally {
                      passkey.reset();
                      control.release();
                    }
                  })();
                }}
              >
                {passkey.isPending && <Spinner size="sm" color="current" />}
                <Trans id="login.passkey">Sign in with a passkey</Trans>
              </Button>
              {!supported && (
                <p className="text-sm text-muted">
                  <Trans id="login.passkey.unsupported">
                    Passkeys are unavailable in this browser or connection. Use
                    your password instead.
                  </Trans>
                </p>
              )}
            </>
          )}
        </>
      )}
    </div>
  );
}

export function Login() {
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  const { data: status } = useSuspenseQuery(authStatusQueryOptions());
  const search = useLocation({ select: (location) => location.searchStr });
  let returnTo: string | undefined;
  let linkError: MessageDescriptor | undefined;
  try {
    returnTo = parseReturnTo(search, window.location.origin);
  } catch (error) {
    linkError = describeError(error);
  }
  return (
    <main className="mx-auto flex w-full max-w-lg flex-col gap-4 px-4 py-8">
      {config.maintenanceMode && (
        <Alert status="warning">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>
              <Trans id="login.maintenance">
                The service is undergoing maintenance. Administrators can still
                try to sign in.
              </Trans>
            </Alert.Title>
            {config.maintenanceMessage && (
              <Alert.Description>{config.maintenanceMessage}</Alert.Description>
            )}
          </Alert.Content>
        </Alert>
      )}
      {linkError ? (
        <Alert status="danger" role="alert">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>
              <FormMessages errors={[linkError]} />
            </Alert.Title>
          </Alert.Content>
        </Alert>
      ) : !status.bootstrapped ? (
        <Alert status="warning">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>
              <Trans id="login.uninitialized">
                This instance has not been initialized.
              </Trans>
            </Alert.Title>
            <Alert.Description>
              <Trans id="login.enroll_instruction">
                Ask the operator to run <code>prohibitorum enroll-admin</code>{" "}
                on the server before signing in.
              </Trans>
            </Alert.Description>
          </Alert.Content>
        </Alert>
      ) : (
        <Card>
          <Card.Content>
            <LoginFlow
              key={returnTo ?? ""}
              config={config}
              returnTo={returnTo}
            />
          </Card.Content>
        </Card>
      )}
    </main>
  );
}
