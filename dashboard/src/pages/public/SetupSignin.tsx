import { Trans, useLingui } from "@lingui/react/macro";
import { browserSupportsWebAuthn } from "@simplewebauthn/browser";
import { useMutation, useSuspenseQuery } from "@tanstack/react-query";
import { useRouter } from "@tanstack/react-router";
import { useEffect, useState } from "react";
import { generateTotpSecret } from "@/api/auth";
import { ApiError, isCancellation } from "@/api/errors";
import {
  setupPasskeyMutationOptions,
  setupPasswordTotpMutationOptions,
} from "@/api/mutations";
import { identitiesQueryOptions, sessionQueryOptions } from "@/api/queries";
import { followRedirect } from "@/app/redirect";
import { Button } from "@/components/custom/Button";
import { PublicStep } from "@/components/custom/PublicStep";
import { PasskeyButton } from "@/pages/public/enroll/PasskeyButton";
import { PasswordTotpSetup } from "@/pages/public/enroll/PasswordTotpSetup";
import { Route } from "@/routes/_public.setup-signin";

function timedOut(error: unknown): boolean {
  return error instanceof ApiError && error.code === "sudo_required";
}

/**
 * A first sign-in of the account's own, offered right after a first sign-in
 * through an upstream provider, so losing that provider does not lock the
 * account out. Every way out — done, skipped, or timed out — goes on to
 * where the sign-in was headed.
 *
 * It asks the server directly rather than through the console's identity
 * check: the session is brand new. If the reader stays long enough for the
 * server to want that check anyway, the step is left for the Security page,
 * and the only way on is to continue.
 */
export function SetupSigninPage() {
  const { redirect = "/" } = Route.useSearch();
  const { data: session } = useSuspenseQuery(sessionQueryOptions());
  const { data: identities } = useSuspenseQuery(identitiesQueryOptions());
  const { i18n } = useLingui();
  const router = useRouter();
  const passkey = useMutation(setupPasskeyMutationOptions());
  const passwordTotp = useMutation(setupPasswordTotpMutationOptions());
  const [mode, setMode] = useState<"choose" | "password-totp">("choose");
  const [expired, setExpired] = useState(false);
  // Which control is taking the page on, so that one alone shows it.
  const [leaving, setLeaving] = useState<"passkey" | "skip" | "continue">();
  // Kept across leaving and coming back to the password setup, so the
  // authenticator the reader already added still matches.
  const [secret] = useState(() => generateTotpSecret());
  // An account with nothing linked has nothing to fall back from. Read once,
  // like the sign-in steps' own check: the list cannot change under the page.
  const [applies] = useState((identities ?? []).length > 0);

  const leave = async (via: "passkey" | "skip" | "continue") => {
    setLeaving(via);
    try {
      await followRedirect(router, redirect);
    } catch (error) {
      setLeaving(undefined);
      throw error;
    }
  };

  useEffect(() => {
    if (!applies) void followRedirect(router, redirect);
  }, [applies, router, redirect]);
  if (!applies || session === null) return null;

  const providers = new Intl.ListFormat(i18n.locale === "zh" ? "zh-CN" : "en", {
    type: "conjunction",
  }).format([
    ...new Set(
      (identities ?? []).map((identity) => identity.providerDisplayName),
    ),
  ]);
  const supported = window.isSecureContext && browserSupportsWebAuthn();
  const busy = passkey.isPending || leaving !== undefined;
  const passkeyPending = passkey.isPending || leaving === "passkey";

  if (mode === "password-totp") {
    return (
      <PasswordTotpSetup
        accountName={session.username}
        secret={secret}
        onBack={() => setMode("choose")}
        submit={async ({ password, secretBase32, code }) => {
          const result = await passwordTotp.mutateAsync({
            password,
            secret_base32: secretBase32,
            code,
          });
          return result.recovery_codes;
        }}
        onSubmitError={(error) => {
          if (!timedOut(error)) return false;
          setExpired(true);
          setMode("choose");
          return true;
        }}
        onDone={() => leave("continue")}
      />
    );
  }

  return (
    <PublicStep
      title={<Trans id="setup_signin.title">Add a way to sign in</Trans>}
      description={
        <Trans id="setup_signin.description">
          Right now you can only sign in through {providers}. Add another way to
          sign in, so losing access to {providers} doesn't lock you out.
        </Trans>
      }
      actions={
        <PublicStep.Actions>
          {expired ? (
            <Button
              fullWidth
              isPending={leaving === "continue"}
              onPress={() => void leave("continue")}
            >
              <Trans id="setup_signin.continue">Continue</Trans>
            </Button>
          ) : (
            <>
              <PasskeyButton
                supported={supported}
                isPending={passkeyPending}
                isDisabled={busy && !passkeyPending}
                onPress={() => {
                  passkey.mutateAsync().then(
                    () => leave("passkey").catch(() => undefined),
                    (error: unknown) => {
                      if (!isCancellation(error) && timedOut(error)) {
                        setExpired(true);
                      }
                    },
                  );
                }}
              >
                <Trans id="setup_signin.passkey">Add a passkey</Trans>
              </PasskeyButton>
              <Button
                variant="secondary"
                fullWidth
                isDisabled={busy}
                onPress={() => setMode("password-totp")}
              >
                <Trans id="enroll.use_password_totp">
                  Use a password and an authenticator instead
                </Trans>
              </Button>
              <Button
                variant="tertiary"
                fullWidth
                isPending={leaving === "skip"}
                isDisabled={busy && leaving !== "skip"}
                onPress={() => void leave("skip")}
              >
                <Trans id="setup_signin.skip">Skip for now</Trans>
              </Button>
            </>
          )}
          <p className="text-center text-sm text-muted">
            <Trans id="setup_signin.later">
              You can also add one later from Security.
            </Trans>
          </p>
        </PublicStep.Actions>
      }
    />
  );
}
