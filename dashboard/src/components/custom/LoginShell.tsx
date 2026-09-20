import { Alert, Button, Card } from "@heroui/react";
import type { MessageDescriptor } from "@lingui/core";
import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { useLocation } from "@tanstack/react-router";
import { ArrowLeft } from "lucide-react";
import type { ReactNode } from "react";
import { useCallback } from "react";
import { parseReturnTo } from "@/api/auth";
import { describeError } from "@/api/errors";
import {
  authStatusQueryOptions,
  publicConfigQueryOptions,
} from "@/api/queries";
import { FormMessages } from "@/components/custom/FormMessages";
import { PageFrame } from "@/components/custom/PageFrame";

export type LoginStep = "password" | "totp" | "recovery";

export type LoginFailure = {
  message: MessageDescriptor;
  secondStep?: boolean;
  reset?: boolean;
};

/** Instance config, bootstrap state and the validated return target the sign-in pages share. */
export function useLoginContext() {
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
  return { config, bootstrapped: status.bootstrapped, returnTo, linkError };
}

/**
 * Chrome shared by the sign-in pages: instance notices, the card, the step
 * heading and the failure alert. The step form renders as children.
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
  const { config, bootstrapped, linkError } = useLoginContext();
  const focusHeading = useCallback((node: HTMLHeadingElement | null) => {
    node?.focus();
  }, []);
  return (
    <PageFrame>
      <div className="mx-auto flex w-full max-w-[30rem] flex-col gap-4">
        {config.maintenanceMode && (
          <Alert status="warning">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Title>
                <Trans id="login.maintenance">
                  The service is undergoing maintenance. Administrators can
                  still try to sign in.
                </Trans>
              </Alert.Title>
              {config.maintenanceMessage && (
                <Alert.Description>
                  {config.maintenanceMessage}
                </Alert.Description>
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
        ) : !bootstrapped ? (
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
              <div className="flex flex-col gap-4">
                <div className="flex items-center gap-2">
                  {step !== "password" && !complete && onBack && (
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
                      isDisabled={busy}
                      onPress={onBack}
                    >
                      <ArrowLeft aria-hidden="true" />
                    </Button>
                  )}
                  <h1
                    key={`${step}-${complete}-${Boolean(failure)}`}
                    ref={focusHeading}
                    className="min-w-0 text-xl font-semibold wrap-anywhere"
                  >
                    {complete ? (
                      <Trans id="login.complete">
                        Authenticator reset complete
                      </Trans>
                    ) : step === "password" ? (
                      <Trans id="login.title">Sign in</Trans>
                    ) : (
                      <Trans id="login.factor.account">
                        Signing in as {username}
                      </Trans>
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
                              authenticator or a passkey. Enter your password
                              again before another code attempt; your old codes
                              may no longer work.
                            </Trans>
                          ) : (
                            <Trans id="login.factor.restart">
                              Enter your password again before trying another
                              code. This verification attempt cannot be reused.
                            </Trans>
                          )}
                        </Alert.Description>
                      )}
                    </Alert.Content>
                  </Alert>
                )}
                {children}
              </div>
            </Card.Content>
          </Card>
        )}
      </div>
    </PageFrame>
  );
}
