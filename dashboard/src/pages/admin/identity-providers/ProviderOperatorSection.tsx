import { Chip, Label, Modal, Radio, RadioGroup, Spinner } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { RotateCw, ShieldCheck } from "lucide-react";
import { useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import {
  startOperatorSessionMutationOptions,
  validateOperatorSessionMutationOptions,
  verifyOperatorSessionMutationOptions,
} from "@/api/mutations";
import type { OperatorSessionView } from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type Provider = {
  slug: string;
  displayName: string;
  secretStatus: string;
  secretValidatedAt: string | null;
};

/**
 * The VRChat account this instance signs in as.
 *
 * VRChat has no client credentials to configure: the instance needs a real
 * account, and the session it establishes is the credential. That is also why
 * this is the only provider where the session has to be replaced by hand when it
 * lapses, and why "log in" and "replace" are the same flow — a second sign-in
 * overwrites what was stored, and until it completes the old session keeps
 * working.
 *
 * The second factor is requested by VRChat, not by this page: the first step
 * submits a username and password, and only a `challenge` answer asks for a
 * code. The password is cleared as soon as it has been used, whatever the
 * outcome, because it is the only field here that is useless afterwards.
 */
export function ProviderOperatorSection({ provider }: { provider: Provider }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const validate = useMutation(
    validateOperatorSessionMutationOptions(queryClient),
  );
  const [signingIn, setSigningIn] = useState(false);
  const [challenge, setChallenge] = useState<OperatorSessionView | null>(null);

  function closeSignIn() {
    setChallenge(null);
    setSigningIn(false);
  }

  const configured = provider.secretStatus !== "unconfigured";

  return (
    <>
      <ConsoleCard>
        <div className="flex flex-col gap-4">
          <div className="flex flex-wrap items-center gap-3">
            <Chip
              size="sm"
              variant="soft"
              color={
                provider.secretStatus === "valid"
                  ? "success"
                  : provider.secretStatus === "invalid"
                    ? "danger"
                    : "default"
              }
            >
              {provider.secretStatus === "valid" ? (
                <Trans id="admin.federation.operator.valid">
                  Session valid
                </Trans>
              ) : provider.secretStatus === "invalid" ? (
                <Trans id="admin.federation.operator.invalid">
                  Session expired
                </Trans>
              ) : (
                <Trans id="admin.federation.operator.none">Not set up</Trans>
              )}
            </Chip>
            {provider.secretValidatedAt !== null && (
              <span className="text-xs text-muted">
                <Trans id="admin.federation.operator.validated">
                  Checked <RelativeTime value={provider.secretValidatedAt} />
                </Trans>
              </span>
            )}
          </div>

          <div className="flex flex-wrap items-center gap-3">
            {configured && (
              <Button
                size="sm"
                variant="outline"
                isPending={validate.isPending}
                onPress={() => validate.mutate(provider.slug)}
              >
                <ShieldCheck size={16} aria-hidden="true" />
                <Trans id="admin.federation.operator.check">Check</Trans>
              </Button>
            )}
            <Button
              size="sm"
              isPending={false}
              onPress={() => {
                setChallenge(null);
                setSigningIn(true);
              }}
            >
              <RotateCw size={16} aria-hidden="true" />
              {configured ? (
                <Trans id="admin.federation.operator.replace">
                  Sign in again
                </Trans>
              ) : (
                <Trans id="admin.federation.operator.login">Sign in</Trans>
              )}
            </Button>
          </div>

          {validate.error !== null && validate.error !== undefined && (
            <SurfaceAlert status="danger" role="alert">
              <SurfaceAlert.Indicator />
              <SurfaceAlert.Content>
                <SurfaceAlert.Title>
                  {t(describeError(validate.error))}
                </SurfaceAlert.Title>
              </SurfaceAlert.Content>
            </SurfaceAlert>
          )}
        </div>
      </ConsoleCard>

      {/* The sign-in is a dialog rather than a section of its own: it is a
          two-step exchange with another service, and leaving it open on the page
          would invite filling in a password and walking away. */}
      <Modal isOpen={signingIn} onOpenChange={(open) => !open && closeSignIn()}>
        <Modal.Backdrop>
          <Modal.Container placement="center" size="md">
            <Modal.Dialog>
              <Modal.Header>
                <Modal.Heading>
                  <Trans id="admin.federation.operator.sign-in-title">
                    Sign in to VRChat
                  </Trans>
                </Modal.Heading>
              </Modal.Header>
              <Modal.Body>
                <OperatorSignInForm
                  slug={provider.slug}
                  challenge={challenge}
                  onChallenge={setChallenge}
                  onClose={closeSignIn}
                />
              </Modal.Body>
            </Modal.Dialog>
          </Modal.Container>
        </Modal.Backdrop>
      </Modal>
    </>
  );
}

/** The dialog's contents; the dialog itself is drawn by the section above. */
function OperatorSignInForm({
  slug,
  challenge,
  onChallenge,
  onClose,
}: {
  slug: string;
  challenge: OperatorSessionView | null;
  onChallenge: (challenge: OperatorSessionView | null) => void;
  onClose: () => void;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const start = useMutation(startOperatorSessionMutationOptions(queryClient));
  const verify = useMutation(verifyOperatorSessionMutationOptions(queryClient));
  const [method, setMethod] = useState("totp");

  const form = useAppForm({
    defaultValues: { username: "", password: "", code: "" },
    onSubmit: async ({ value }) => {
      try {
        if (challenge === null) {
          const result = await start.mutateAsync({
            slug,
            body: { username: value.username, password: value.password },
          });
          // The password has done its work; keeping it in the form would leave
          // a live credential on screen behind the challenge step.
          form.setFieldValue("password", "");
          if (result.status === "challenge") onChallenge(result);
          else onClose();
          return;
        }
        const result = await verify.mutateAsync({
          slug,
          body: {
            challenge: challenge.challenge ?? "",
            method,
            code: value.code,
          },
        });
        if (result.status === "valid") onClose();
        else onChallenge(result);
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: {
            vrchat_operator_credentials_invalid: "password",
            vrchat_operator_code_invalid: "code",
            vrchat_operator_challenge_invalid: "username",
          },
          locations: {},
        });
      }
    },
  });

  const methods = challenge?.methods ?? [];

  return (
    <>
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.federation.operator.form",
            message: "VRChat sign-in",
          })}
          className="flex flex-col gap-4"
        >
          <form.FormError />

          {challenge === null ? (
            <>
              <form.AppField name="username">
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="admin.federation.operator.username">
                        Username
                      </Trans>
                    }
                    autoComplete="off"
                    variant="secondary"
                  />
                )}
              </form.AppField>
              <form.AppField name="password">
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="admin.federation.operator.password">
                        Password
                      </Trans>
                    }
                    type="password"
                    autoComplete="off"
                    variant="secondary"
                  />
                )}
              </form.AppField>
            </>
          ) : (
            <>
              <RadioGroup
                value={method}
                variant="secondary"
                onChange={(next) => setMethod(next)}
              >
                <Label>
                  <Trans id="admin.federation.operator.method">
                    Verification method
                  </Trans>
                </Label>
                {methods.map((candidate) => (
                  <Radio key={candidate} value={candidate}>
                    <Radio.Content>
                      <Radio.Control>
                        <Radio.Indicator />
                      </Radio.Control>
                      {candidate === "totp" ? (
                        <Trans id="admin.federation.operator.method.totp">
                          Authenticator app
                        </Trans>
                      ) : candidate === "emailOtp" ? (
                        <Trans id="admin.federation.operator.method.email">
                          Email code
                        </Trans>
                      ) : (
                        <Trans id="admin.federation.operator.method.otp">
                          Code
                        </Trans>
                      )}
                    </Radio.Content>
                  </Radio>
                ))}
              </RadioGroup>

              {method === "otp" ? (
                <form.AppField name="code">
                  {(field) => (
                    <field.FormField
                      label={
                        <Trans id="admin.federation.operator.code">Code</Trans>
                      }
                      autoComplete="one-time-code"
                      variant="secondary"
                    />
                  )}
                </form.AppField>
              ) : (
                <form.AppField name="code">
                  {(field) => (
                    <field.OtpField
                      digits={6}
                      variant="secondary"
                      label={
                        <Trans id="admin.federation.operator.code">Code</Trans>
                      }
                    />
                  )}
                </form.AppField>
              )}
            </>
          )}

          {(start.isPending || verify.isPending) && (
            <div className="flex items-center gap-2 text-xs text-muted">
              <Spinner size="sm" />
              <Trans id="admin.federation.operator.pending">
                Waiting for VRChat…
              </Trans>
            </div>
          )}

          <div className="flex items-center gap-2">
            <form.SubmitButton>
              {challenge === null ? (
                <Trans id="admin.federation.operator.submit">Sign in</Trans>
              ) : (
                <Trans id="admin.federation.operator.verify">Verify</Trans>
              )}
            </form.SubmitButton>
            <Button variant="tertiary" onPress={onClose}>
              <Trans id="confirm.cancel">Cancel</Trans>
            </Button>
          </div>
        </form.Form>
      </form.AppForm>
    </>
  );
}
