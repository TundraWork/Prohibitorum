import { Checkbox } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import type { HistoryState } from "@tanstack/react-router";
import { useLocation, useNavigate, useRouter } from "@tanstack/react-router";
import { Fingerprint, MonitorSmartphone } from "lucide-react";
import {
  useCallback,
  useEffect,
  useEffectEvent,
  useRef,
  useState,
} from "react";
import {
  buildTotpUri,
  cancelPasskeyAuthentication,
  generateTotpSecret,
  isPasskeyAutofillAvailable,
  isValidLoginPassword,
  isValidRecoveryCode,
  isValidTotpCode,
  type PasskeyAssertion,
  waitForPasskeyAutofill,
} from "@/api/auth";
import { describeError, isCancellation } from "@/api/errors";
import {
  passkeyAutofillMutationOptions,
  passkeyMutationOptions,
  passwordMutationOptions,
  recoveryMutationOptions,
  totpMutationOptions,
} from "@/api/mutations";
import {
  clearSessionQueries,
  publicFederationProvidersQueryOptions,
} from "@/api/queries";
import type {
  PublicConfig,
  RecoveryRequest,
  RecoveryResult,
} from "@/api/raw-paths";
import { followRedirect } from "@/app/redirect";
import { withRouterSkipLoading } from "@/app/router";
import { Button } from "@/components/custom/Button";
import type { LoginFailure } from "@/components/custom/LoginShell";
import { LoginShell, useLoginContext } from "@/components/custom/LoginShell";
import { OrSeparator } from "@/components/custom/OrSeparator";
import { OtpField } from "@/components/custom/OtpField";
import { ProviderButtons } from "@/components/custom/ProviderButtons";
import { RecoveryCodes } from "@/components/custom/RecoveryCodes";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
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
  const router = useRouter();
  const [busy, setBusy] = useState(false);
  const [finishing, setFinishing] = useState(false);
  const [held, setHeld] = useState(false);
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
  // A provider holds the flow while it takes the page away, and lets go when
  // the page comes back from the back-forward cache.
  const heldRef = useRef(false);
  const hold = useCallback((leaving: boolean) => {
    heldRef.current = leaving;
    setHeld(leaving);
  }, []);
  const control: FlowControl = {
    busy: busy || finishing || held,
    acquire: () => {
      if (locked.current || heldRef.current || finishing || !active.current)
        return false;
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
      setFinishing(true);
      await clearSessionQueries(queryClient);
      if (!active.current) return;
      await withRouterSkipLoading(router, () =>
        followRedirect(router, redirect),
      );
    } catch (error) {
      setFinishing(false);
      setFailure({ message: describeError(error) });
      throw error;
    }
  }
  return { control, finishing, failure, setFailure, finish, hold };
}

type LoginFlow = ReturnType<typeof useLoginFlow>;

/**
 * Offers passkeys in the username field's autofill while the first step is
 * open, and signs in with the one the reader picks.
 *
 * Waiting reports nothing: the reader has pressed nothing, so autofill not
 * appearing is the whole of a failure there, and it is not tried again. Once a
 * passkey is picked, finishing takes the flow like the passkey button does and
 * fails the same way; autofill is then offered again. The button calls
 * `pause` before its own ceremony and `resume` after it, since the browser
 * runs one ceremony at a time.
 */
function usePasskeyAutofill({
  flow,
  returnTo,
}: {
  flow: LoginFlow;
  returnTo?: string;
}) {
  const completion = useMutation(passkeyAutofillMutationOptions(returnTo));
  // Each value starts a new wait; null while the button's ceremony runs.
  const [run, setRun] = useState<number | null>(0);
  const waiting = useRef<AbortController>(undefined);
  const restart = useCallback(
    () => setRun((current) => (current === null ? null : current + 1)),
    [],
  );
  const signIn = useEffectEvent(async (assertion: PasskeyAssertion) => {
    if (!flow.control.acquire()) {
      restart();
      return;
    }
    let signedIn = false;
    try {
      const result = await completion.mutateAsync(assertion);
      if (flow.control.isActive()) await flow.finish(result.redirect);
      signedIn = true;
    } catch (error) {
      if (flow.control.isActive() && !isCancellation(error))
        flow.setFailure({ message: describeError(error) });
    } finally {
      completion.reset();
      flow.control.release();
      if (!signedIn) restart();
    }
  });
  useEffect(() => {
    if (run === null) return;
    const controller = new AbortController();
    waiting.current = controller;
    const { signal } = controller;
    void (async () => {
      let assertion: PasskeyAssertion;
      try {
        if (!(await isPasskeyAutofillAvailable())) return;
        assertion = await waitForPasskeyAutofill(signal);
      } catch {
        return;
      }
      if (!signal.aborted) await signIn(assertion);
    })();
    return () => {
      controller.abort();
      if (waiting.current === controller) waiting.current = undefined;
    };
  }, [run]);
  const pause = useCallback(() => {
    waiting.current?.abort();
    setRun(null);
  }, []);
  const resume = useCallback(
    () => setRun((current) => (current === null ? 0 : current)),
    [],
  );
  return { pause, resume };
}

/** Sends a factor route opened without a password result back to the first step. */
function usePasswordResult(token: string | undefined): boolean {
  const navigate = useNavigate();
  // The token cannot change while this page is mounted, so read it once:
  // re-reading it during unmount would navigate away from the step being entered.
  const [ready] = useState(token !== undefined);
  useEffect(() => {
    if (!ready) void navigate({ to: "/login", search: true, replace: true });
  }, [ready, navigate]);
  return ready;
}

function PasswordForm({
  control,
  username,
  submitVariant,
  onSuccess,
  onFailure,
}: {
  control: FlowControl;
  username: string;
  submitVariant: "primary" | "secondary";
  onSuccess: (username: string, token: string) => void | Promise<void>;
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
          await onSuccess(value.username, result.partial_session_token);
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
                autoComplete="username webauthn"
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
          <form.SubmitButton fullWidth variant={submitVariant}>
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
  onFailure: (error: unknown, reset: boolean) => void | Promise<void>;
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
        if (control.isActive()) await onFailure(error, resetting);
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
                <SurfaceAlert status="warning">
                  <SurfaceAlert.Indicator />
                  <SurfaceAlert.Content>
                    <SurfaceAlert.Title>
                      <Trans id="login.reset.warning">
                        After a successful reset, your old authenticator and all
                        old recovery codes will stop working.
                      </Trans>
                    </SurfaceAlert.Title>
                  </SurfaceAlert.Content>
                </SurfaceAlert>
                <TotpSetup secret={setup.secret} uri={setup.uri} onSurface />
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
  const router = useRouter();
  const passkey = useMutation(passkeyMutationOptions(returnTo));
  const resetPasskey = passkey.reset;
  useEffect(
    () => () => {
      cancelPasskeyAuthentication();
      resetPasskey();
    },
    [resetPasskey],
  );
  const autofill = usePasskeyAutofill({ flow, returnTo });
  const supported = window.isSecureContext && browserSupportsWebAuthn();
  return (
    <LoginShell step="password" busy={flow.control.busy} failure={flow.failure}>
      <Button
        variant={supported ? "primary" : "secondary"}
        fullWidth
        isPending={passkey.isPending}
        isDisabled={!supported || (flow.control.busy && !passkey.isPending)}
        onPress={() => {
          if (!flow.control.acquire()) return;
          autofill.pause();
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
              autofill.resume();
            }
          })();
        }}
      >
        <Fingerprint size={16} aria-hidden="true" />
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
      <OrSeparator />
      <PasswordForm
        control={flow.control}
        username={username ?? ""}
        submitVariant={supported ? "secondary" : "primary"}
        onSuccess={async (username, token) => {
          flow.setFailure(undefined);
          await withRouterSkipLoading(router, () =>
            navigate({
              to: "/login/totp",
              search: true,
              state: loginState({ username, token }),
            }),
          );
        }}
        onFailure={(error) =>
          flow.setFailure({ message: describeError(error) })
        }
      />
      <OtherSignIns
        returnTo={returnTo}
        busy={flow.control.busy}
        onLeavingChange={flow.hold}
      />
    </LoginShell>
  );
}

/**
 * The ways to sign in that start somewhere else, under the local ones: from a
 * device that is signed in already, then through each upstream provider. A
 * provider is left through the server, carrying the page's `return_to` so the
 * sign-in still ends where it was headed.
 *
 * During maintenance only administrators reach this page, and pairing sits
 * behind the maintenance guard, so the device button is left out. With
 * neither the device button nor a provider, nothing is drawn, the separator
 * included.
 */
function OtherSignIns({
  returnTo,
  busy,
  onLeavingChange,
}: {
  returnTo?: string;
  busy: boolean;
  onLeavingChange: (leaving: boolean) => void;
}) {
  const { config } = useLoginContext();
  const navigate = useNavigate();
  const { data: providers } = useSuspenseQuery(
    publicFederationProvidersQueryOptions(),
  );
  const pairing = !config.maintenanceMode;
  if (!pairing && providers.length === 0) return null;
  return (
    <>
      <OrSeparator />
      <div className="flex flex-col gap-2">
        {pairing && (
          <Button
            variant="secondary"
            fullWidth
            isDisabled={busy}
            onPress={() =>
              void navigate({
                to: "/pair",
                search: returnTo === undefined ? {} : { return_to: returnTo },
              })
            }
          >
            <MonitorSmartphone size={16} aria-hidden="true" />
            <Trans id="login.other_device">Sign in with another device</Trans>
          </Button>
        )}
        <ProviderButtons
          providers={providers}
          isDisabled={busy}
          onLeavingChange={onLeavingChange}
          href={(provider) => {
            const address = `/api/prohibitorum/auth/federation/${encodeURIComponent(provider.slug)}/login`;
            return returnTo === undefined
              ? address
              : `${address}?${new URLSearchParams({ return_to: returnTo })}`;
          }}
        />
      </div>
    </>
  );
}

export function TotpPage() {
  const { username, token } = useLoginRouteState() ?? {};
  const { config, returnTo } = useLoginContext();
  const flow = useLoginFlow();
  const navigate = useNavigate();
  const router = useRouter();
  const ready = usePasswordResult(token);
  if (!ready) return null;
  return (
    <LoginShell
      step="totp"
      username={username}
      busy={flow.control.busy}
      failure={flow.failure}
      onBack={() =>
        void withRouterSkipLoading(router, () =>
          navigate({ to: "/login", search: true }),
        )
      }
    >
      <FactorForm
        control={flow.control}
        mode="totp"
        config={config}
        username={username ?? ""}
        returnTo={returnTo}
        token={token ?? ""}
        onFailure={async (error, reset) => {
          await withRouterSkipLoading(router, () =>
            navigate({
              to: "/login",
              search: true,
              state: loginState({
                username,
                failure: {
                  message: describeError(error),
                  secondStep: true,
                  reset,
                },
              }),
            }),
          );
        }}
        onSuccess={(redirect) => {
          flow.setFailure(undefined);
          return flow.finish(redirect);
        }}
        onSwitch={() =>
          void withRouterSkipLoading(router, () =>
            navigate({
              to: "/login/recovery",
              search: true,
              state: loginState({ username, token }),
            }),
          )
        }
      />
    </LoginShell>
  );
}

export function RecoveryPage() {
  const { username, token } = useLoginRouteState() ?? {};
  const { config, returnTo } = useLoginContext();
  const flow = useLoginFlow();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const router = useRouter();
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
        void withRouterSkipLoading(router, () =>
          navigate({
            to: "/login/totp",
            search: true,
            state: loginState({ username, token }),
          }),
        )
      }
    >
      {savedCodes ? (
        <RecoveryCodes
          codes={savedCodes.codes}
          onContinue={() => flow.finish(savedCodes.redirect)}
          onSurface
        />
      ) : (
        <FactorForm
          control={flow.control}
          mode="recovery"
          config={config}
          username={username ?? ""}
          returnTo={returnTo}
          token={token ?? ""}
          onFailure={async (error, reset) => {
            await withRouterSkipLoading(router, () =>
              navigate({
                to: "/login",
                search: true,
                state: loginState({
                  username,
                  failure: {
                    message: describeError(error),
                    secondStep: true,
                    reset,
                  },
                }),
              }),
            );
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
