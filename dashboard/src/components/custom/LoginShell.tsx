import type { MessageDescriptor } from "@lingui/core";
import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { useSearch } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { readReturnTo } from "@/api/auth";
import { describeError } from "@/api/errors";
import {
  authStatusQueryOptions,
  loginContextQueryOptions,
  publicConfigQueryOptions,
} from "@/api/queries";
import { FormMessages } from "@/components/custom/FormMessages";
import { PublicStep } from "@/components/custom/PublicStep";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";

export type LoginStep = "password" | "totp" | "recovery";

export type LoginFailure = {
  message: MessageDescriptor;
  secondStep?: boolean;
  reset?: boolean;
};

/**
 * Instance config, bootstrap state and the validated return target the sign-in
 * pages share. When the link points at this instance's own authorize or SSO
 * endpoints, `application` names the registered app the sign-in continues to;
 * it stays undefined while that read is in flight or has nothing to say, so
 * the pages never wait on it.
 */
export function useLoginContext() {
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  const { data: status } = useSuspenseQuery(authStatusQueryOptions());
  // The three sign-in steps share this, so it reads whichever one is showing.
  const rawReturnTo = useSearch({
    strict: false,
    select: (search) => search.return_to,
  });
  let returnTo: string | undefined;
  let linkError: MessageDescriptor | undefined;
  try {
    returnTo = readReturnTo(rawReturnTo);
  } catch (error) {
    linkError = describeError(error);
  }
  // A protocol's login bounce always carries this origin's absolute
  // /oauth/authorize, /saml/sso or /saml/sso/init URL, and the server answers
  // anything else with 400 — so anything else is not asked, keeping the
  // request for the links that can actually name an application.
  const contextTarget =
    returnTo !== undefined && !linkError
      ? loginContextTarget(returnTo, window.location.origin)
      : undefined;
  const context = useQuery({
    ...loginContextQueryOptions(contextTarget ?? ""),
    enabled: contextTarget !== undefined,
  });
  const application = context.data?.application
    ? context.data.application.label
    : undefined;
  return {
    config,
    bootstrapped: status.bootstrapped,
    returnTo,
    linkError,
    application,
  };
}

/**
 * The return_to the login-context endpoint is asked about, or undefined when
 * it cannot name an application: the target must be this origin's authorize or
 * SAML endpoint. An unparsable value is left to the server's 400.
 */
export function loginContextTarget(returnTo: string, origin: string) {
  let target: URL | null = null;
  try {
    target = new URL(returnTo);
  } catch {
    return undefined;
  }
  if (
    target.origin !== origin ||
    (target.pathname !== "/oauth/authorize" &&
      target.pathname !== "/saml/sso" &&
      target.pathname !== "/saml/sso/init")
  ) {
    return undefined;
  }
  return returnTo;
}

/**
 * Chrome shared by the sign-in pages: instance notices, the step heading and
 * the failure alert. The card frame comes from the public layout; the step
 * form renders as children.
 */
export function LoginShell({
  step,
  username,
  busy,
  failure,
  complete = false,
  onBack,
  children,
}: {
  step: LoginStep;
  username?: string;
  busy: boolean;
  failure?: LoginFailure;
  complete?: boolean;
  onBack?: () => void;
  children: ReactNode;
}) {
  const { t } = useLingui();
  const { config, bootstrapped, linkError, application } = useLoginContext();
  return (
    <div className="mx-auto flex w-full max-w-[30rem] flex-col gap-4">
      {config.maintenanceMode && (
        <SurfaceAlert status="warning">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              <Trans id="login.maintenance.admin_only">
                The service is undergoing maintenance. Only administrators can
                sign in.
              </Trans>
            </SurfaceAlert.Title>
            {config.maintenanceMessage && (
              <SurfaceAlert.Description>
                {config.maintenanceMessage}
              </SurfaceAlert.Description>
            )}
          </SurfaceAlert.Content>
        </SurfaceAlert>
      )}
      {linkError ? (
        <SurfaceAlert status="danger" role="alert">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              <FormMessages errors={[linkError]} />
            </SurfaceAlert.Title>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      ) : !bootstrapped ? (
        <SurfaceAlert status="warning">
          <SurfaceAlert.Indicator />
          <SurfaceAlert.Content>
            <SurfaceAlert.Title>
              <Trans id="login.uninitialized">
                This instance has not been initialized.
              </Trans>
            </SurfaceAlert.Title>
            <SurfaceAlert.Description>
              <Trans id="login.enroll_instruction">
                Ask the operator to run <code>prohibitorum enroll-admin</code>{" "}
                on the server before signing in.
              </Trans>
            </SurfaceAlert.Description>
          </SurfaceAlert.Content>
        </SurfaceAlert>
      ) : (
        <PublicStep
          titleKey={`${step}-${complete}-${Boolean(failure)}`}
          back={
            step !== "password" && !complete && onBack
              ? {
                  label:
                    step === "recovery"
                      ? t({
                          id: "login.use_totp",
                          message: "Use an authenticator code",
                        })
                      : t({ id: "login.back", message: "Back to password" }),
                  onPress: onBack,
                  isDisabled: busy,
                }
              : undefined
          }
          title={
            complete ? (
              <Trans id="login.complete">Authenticator reset complete</Trans>
            ) : step === "password" ? (
              application === undefined ? (
                <Trans id="login.title">Sign in</Trans>
              ) : (
                <Trans id="login.title.application">
                  Sign in to continue to {application}
                </Trans>
              )
            ) : (
              <Trans id="login.factor.account">Signing in as {username}</Trans>
            )
          }
        >
          {failure && (
            <SurfaceAlert status="danger" role="alert">
              <SurfaceAlert.Indicator />
              <SurfaceAlert.Content>
                <SurfaceAlert.Title>
                  <FormMessages errors={[failure.message]} />
                </SurfaceAlert.Title>
                {failure.secondStep && (
                  <SurfaceAlert.Description>
                    {failure.reset ? (
                      <Trans id="login.reset.uncertain">
                        The reset may already have completed. Try your new
                        authenticator or a passkey. Enter your password again
                        before another code attempt; your old codes may no
                        longer work.
                      </Trans>
                    ) : (
                      <Trans id="login.factor.restart">
                        Enter your password again before trying another code.
                        This verification attempt cannot be reused.
                      </Trans>
                    )}
                  </SurfaceAlert.Description>
                )}
              </SurfaceAlert.Content>
            </SurfaceAlert>
          )}
          {children}
        </PublicStep>
      )}
    </div>
  );
}
