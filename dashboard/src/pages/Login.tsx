import { Button, Checkbox, Spinner } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { HistoryState } from "@tanstack/react-router";
import { useLocation, useNavigate } from "@tanstack/react-router";
import { useEffect, useRef, useState } from "react";
import {
  buildTotpUri,
  cancelPasskeyAuthentication,
  generateTotpSecret,
  isValidLoginPassword,
  isValidRecoveryCode,
  isValidTotpCode,
  validateRedirect,
} from "@/api/auth";
import { describeError, isCancellation } from "@/api/errors";
import {
  passkeyMutationOptions,
  passwordMutationOptions,
  recoveryMutationOptions,
  totpMutationOptions,
} from "@/api/mutations";
import { clearSessionQueries } from "@/api/queries";
import type {
  PublicConfig,
  RecoveryRequest,
  RecoveryResult,
} from "@/api/raw-paths";
import type { LoginFailure } from "@/components/custom/LoginShell";
import { LoginShell, useLoginContext } from "@/components/custom/LoginShell";
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

/** Sign-in progress carried between the step routes through the history entry. */
type LoginRouteState = {
  username?: string;
  token?: string;
  failure?: LoginFailure;
};

type LoginHistoryState = { login?: LoginRouteState };

type FlowControl = {
  busy: boolean;
  acquire: () => boolean;
  release: () => void;
  isActive: () => boolean;
};

/** TanStack Router types extra history state through an unresolvable module, so this cast carries it. */
function loginState(login: LoginRouteState): HistoryState {
  return { login } as HistoryState;
}

function useLoginRouteState(): LoginRouteState | undefined {
  return useLocation({
    select: (location) => (location.state as LoginHistoryState).login,
  });
}

function useLoginFlow(initialFailure?: LoginFailure) {
  const queryClient = useQueryClient();
  const [busy, setBusy] = useState(false);
  const [finishing, setFinishing] = useState(false);
  const [failure, setFailure] = useState<LoginFailure | undefined>(
    initialFailure,
  );
  const locked = useRef(false);
  const active = useRef(false);
  useEffect(() => {
    active.current = true;
    return () => {
      active.current = false;
    };
  }, []);
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
      window.location.assign(destination);
    } catch (error) {
      setFinishing(false);
      setFailure({ message: describeError(error) });
      throw error;
    }
  }
  return { control, finishing, failure, setFailure, finish };
}

/** Sends a factor route opened without a password result back to the first step. */
function usePasswordResult(token: string | undefined): boolean {
  const navigate = useNavigate();
  // The token cannot change while this page is mounted, so read it once:
  // re-reading it during unmount would navigate away from the step being entered.
  const [ready] = useState(token !== undefined);
  useEffect(() => {
    if (!ready) void navigate({ to: "/login", replace: true });
  }, [ready, navigate]);
  return ready;
}

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
              onBlur: ({ value }) =>
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
                variant="secondary"
              />
            )}
          </form.AppField>
          <form.AppField
            name="password"
            validators={{
              onBlur: ({ value }) =>
                isValidLoginPassword(value) ? undefined : passwordInvalid,
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="login.password">Password</Trans>}
                type="password"
                autoComplete="current-password"
                isDisabled={control.busy}
                variant="secondary"
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
  token,
  onFailure,
  onSuccess,
  onSwitch,
}: {
  control: FlowControl;
  mode: "totp" | "recovery";
  config: PublicConfig;
  username: string;
  returnTo?: string;
  token: string;
  onFailure: (error: unknown, reset: boolean) => void;
  onSuccess: (redirect: string, codes?: string[]) => Promise<void>;
  onSwitch?: () => void;
}) {
  const { t } = useLingui();
  const totp = useMutation(totpMutationOptions(returnTo));
  const recovery = useMutation(recoveryMutationOptions(returnTo));
  const [setup, setSetup] = useState<{ secret: string; uri: string }>();
  const form = useAppForm({
    defaultValues: { code: "", totpCode: "" },
    onSubmit: async ({ value, formApi }) => {
      if (!control.acquire()) return;
      const resetting = setup !== undefined;
      try {
        const result: RecoveryResult =
          mode === "totp"
            ? await totp.mutateAsync({
                partial_session_token: token,
                code: value.code,
              })
            : await recovery.mutateAsync(
                setup
                  ? ({
                      partial_session_token: token,
                      code: value.code,
                      reset_authenticator: true,
                      totp_secret_base32: setup.secret,
                      totp_code: value.totpCode,
                    } satisfies RecoveryRequest)
                  : ({
                      partial_session_token: token,
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
            onBlur: ({ value }) =>
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
                variant="secondary"
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
                variant="secondary"
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
                    onBlur: ({ value }) =>
                      isValidTotpCode(value, config.totp.digits)
                        ? undefined
                        : codeInvalid,
                  }}
                >
                  {() => (
                    <OtpField
                      digits={config.totp.digits}
                      variant="secondary"
                      label={
                        <Trans id="login.reset.code">
                          New authenticator code
                        </Trans>
                      }
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
          {mode === "totp" && onSwitch && (
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

export function PasswordPage() {
  const { username, failure } = useLoginRouteState() ?? {};
  const { returnTo } = useLoginContext();
  const flow = useLoginFlow(failure);
  const navigate = useNavigate();
  const passkey = useMutation(passkeyMutationOptions(returnTo));
  const resetPasskey = passkey.reset;
  useEffect(
    () => () => {
      cancelPasskeyAuthentication();
      resetPasskey();
    },
    [resetPasskey],
  );
  const supported = window.isSecureContext && browserSupportsWebAuthn();
  return (
    <LoginShell step="password" busy={flow.control.busy} failure={flow.failure}>
      <PasswordForm
        control={flow.control}
        username={username ?? ""}
        onSuccess={(username, token) => {
          flow.setFailure(undefined);
          void navigate({
            to: "/login/totp",
            state: loginState({ username, token }),
          });
        }}
        onFailure={(error) =>
          flow.setFailure({ message: describeError(error) })
        }
      />
      <Button
        variant="secondary"
        fullWidth
        isPending={passkey.isPending}
        isDisabled={!supported || (flow.control.busy && !passkey.isPending)}
        onPress={() => {
          if (!flow.control.acquire()) return;
          void (async () => {
            try {
              const result = await passkey.mutateAsync();
              if (flow.control.isActive()) await flow.finish(result.redirect);
            } catch (error) {
              if (flow.control.isActive() && !isCancellation(error))
                flow.setFailure({ message: describeError(error) });
            } finally {
              passkey.reset();
              flow.control.release();
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
            Passkeys are unavailable in this browser or connection. Use your
            password instead.
          </Trans>
        </p>
      )}
    </LoginShell>
  );
}

export function TotpPage() {
  const { username, token } = useLoginRouteState() ?? {};
  const { config, returnTo } = useLoginContext();
  const flow = useLoginFlow();
  const navigate = useNavigate();
  const ready = usePasswordResult(token);
  if (!ready) return null;
  return (
    <LoginShell
      step="totp"
      username={username}
      busy={flow.control.busy}
      failure={flow.failure}
      onBack={() => void navigate({ to: "/login" })}
    >
      {flow.finishing ? (
        <Spinner />
      ) : (
        <FactorForm
          control={flow.control}
          mode="totp"
          config={config}
          username={username ?? ""}
          returnTo={returnTo}
          token={token ?? ""}
          onFailure={(error, reset) => {
            void navigate({
              to: "/login",
              state: loginState({
                username,
                failure: {
                  message: describeError(error),
                  secondStep: true,
                  reset,
                },
              }),
            });
          }}
          onSuccess={(redirect) => {
            flow.setFailure(undefined);
            return flow.finish(redirect);
          }}
          onSwitch={() =>
            void navigate({
              to: "/login/recovery",
              state: loginState({ username, token }),
            })
          }
        />
      )}
    </LoginShell>
  );
}

export function RecoveryPage() {
  const { username, token } = useLoginRouteState() ?? {};
  const { config, returnTo } = useLoginContext();
  const flow = useLoginFlow();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [savedCodes, setSavedCodes] = useState<{
    redirect: string;
    codes: string[];
  }>();
  const ready = usePasswordResult(token);
  if (!ready) return null;
  return (
    <LoginShell
      step="recovery"
      username={username}
      busy={flow.control.busy}
      failure={flow.failure}
      complete={savedCodes !== undefined}
      onBack={() =>
        void navigate({
          to: "/login/totp",
          state: loginState({ username, token }),
        })
      }
    >
      {savedCodes ? (
        <RecoveryCodes
          codes={savedCodes.codes}
          onContinue={() => flow.finish(savedCodes.redirect)}
        />
      ) : flow.finishing ? (
        <Spinner />
      ) : (
        <FactorForm
          control={flow.control}
          mode="recovery"
          config={config}
          username={username ?? ""}
          returnTo={returnTo}
          token={token ?? ""}
          onFailure={(error, reset) => {
            void navigate({
              to: "/login",
              state: loginState({
                username,
                failure: {
                  message: describeError(error),
                  secondStep: true,
                  reset,
                },
              }),
            });
          }}
          onSuccess={async (redirect, codes) => {
            flow.setFailure(undefined);
            if (codes) {
              setSavedCodes({ redirect, codes });
              await clearSessionQueries(queryClient);
            } else await flow.finish(redirect);
          }}
        />
      )}
    </LoginShell>
  );
}
