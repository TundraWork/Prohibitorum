import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { CircleAlertIcon, RotateCwIcon, ShieldCheckIcon } from "lucide-react";
import { useId, useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import {
  startOperatorSessionMutationOptions,
  validateOperatorSessionMutationOptions,
  verifyOperatorSessionMutationOptions,
} from "@/api/mutations";
import type {
  OperatorSessionStartRequest,
  OperatorSessionVerifyRequest,
  OperatorSessionView,
} from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SettingRow } from "@/components/custom/SettingRow";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import { Alert, AlertTitle } from "@/components/ui/alert";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Label } from "@/components/ui/label";
import { Radio, RadioGroup } from "@/components/ui/radio-group";
import { Spinner } from "@/components/ui/spinner";
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
 * lapses, and why "sign in" and "sign in again" are the same flow — a second
 * sign-in overwrites what was stored, and until it completes the old session
 * keeps working.
 *
 * The second factor is requested by VRChat, not by this page: the first step
 * submits a username and password, and only a `challenge` answer asks for a
 * code. The requests are made here, not in the dialog, so closing it never
 * drops an outcome; a refusal is shown in the dialog, which stays open, and
 * the dialog ignores dismissal while a request is out.
 */
export function ProviderOperatorSection({ provider }: { provider: Provider }) {
  const queryClient = useQueryClient();
  const { t } = useLingui();
  // A failed check is told under the row, where the reader pressed it.
  const validate = useMutation(
    withInlineReport(validateOperatorSessionMutationOptions(queryClient), true),
  );
  const start = useMutation(
    withInlineReport(startOperatorSessionMutationOptions(queryClient), true),
  );
  const verify = useMutation(
    withInlineReport(verifyOperatorSessionMutationOptions(queryClient), true),
  );
  const [open, setOpen] = useState(false);
  const [challenge, setChallenge] = useState<OperatorSessionView | null>(null);

  const configured = provider.secretStatus !== "unconfigured";
  const busy = start.isPending || verify.isPending;

  function openSignIn() {
    setChallenge(null);
    setOpen(true);
  }

  return (
    <>
      <SettingRow
        title={<SessionBadge status={provider.secretStatus} />}
        description={
          provider.secretValidatedAt === null ? undefined : (
            <Trans id="admin.federation.operator.validated">
              Checked <RelativeTime value={provider.secretValidatedAt} />
            </Trans>
          )
        }
      >
        <div className="flex flex-wrap items-center gap-2">
          {configured && (
            <Button
              pending={validate.isPending}
              onClick={() => validate.mutate(provider.slug)}
              variant="outline"
            >
              <ShieldCheckIcon aria-hidden="true" />
              <Trans id="admin.federation.operator.check">Check</Trans>
            </Button>
          )}
          <Button onClick={openSignIn}>
            <RotateCwIcon aria-hidden="true" />
            {configured ? (
              <Trans id="admin.federation.operator.replace">
                Sign in again
              </Trans>
            ) : (
              <Trans id="admin.federation.operator.login">Sign in</Trans>
            )}
          </Button>
        </div>
      </SettingRow>
      {validate.error !== null && (
        <Alert variant="error">
          <CircleAlertIcon aria-hidden="true" />
          <AlertTitle>{t(describeError(validate.error))}</AlertTitle>
        </Alert>
      )}

      {/* A dialog rather than fields on the page: it is a two-step exchange
          with another service, and a password form left open on the page
          would invite walking away from it. */}
      <Dialog
        open={open}
        // Once VRChat has sent a code, closing would throw the step away and the
        // next sign-in would send another; only Cancel ends it.
        onOpenChange={(next) =>
          !next && !busy && challenge === null && setOpen(false)
        }
      >
        <DialogPopup
          className="sm:max-w-md"
          showCloseButton={!busy && challenge === null}
          portalProps={{ container: getTopLayerContainer() }}
        >
          <OperatorSignInForm
            challenge={challenge}
            busy={busy}
            onStart={(body) => start.mutateAsync({ slug: provider.slug, body })}
            onVerify={(body) =>
              verify.mutateAsync({ slug: provider.slug, body })
            }
            onChallenge={setChallenge}
            onClose={() => setOpen(false)}
          />
        </DialogPopup>
      </Dialog>
    </>
  );
}

function SessionBadge({ status }: { status: string }) {
  if (status === "valid") {
    return (
      <Badge variant="success">
        <Trans id="admin.federation.operator.valid">Session valid</Trans>
      </Badge>
    );
  }
  if (status === "invalid") {
    return (
      <Badge variant="error">
        <Trans id="admin.federation.operator.invalid">Session expired</Trans>
      </Badge>
    );
  }
  return (
    <Badge variant="outline">
      <Trans id="admin.federation.operator.none">Not set up</Trans>
    </Badge>
  );
}

/**
 * The dialog's contents. It mounts with the popup, so a closed dialog's typed
 * values are gone and the next one starts empty. The password is cleared as
 * soon as it has been used, whatever the outcome, because it is the only field
 * here that is useless afterwards.
 */
function OperatorSignInForm({
  challenge,
  busy,
  onStart,
  onVerify,
  onChallenge,
  onClose,
}: {
  challenge: OperatorSessionView | null;
  busy: boolean;
  onStart: (body: OperatorSessionStartRequest) => Promise<OperatorSessionView>;
  onVerify: (
    body: OperatorSessionVerifyRequest,
  ) => Promise<OperatorSessionView>;
  onChallenge: (challenge: OperatorSessionView | null) => void;
  onClose: () => void;
}) {
  const { t } = useLingui();
  const methodLabelId = useId();
  const methods = challenge?.methods ?? [];
  const [chosen, setChosen] = useState<string | undefined>();
  const method =
    chosen !== undefined && methods.includes(chosen)
      ? chosen
      : (methods[0] ?? "totp");

  const form = useAppForm({
    defaultValues: { username: "", password: "", code: "" },
    onSubmit: async ({ value }) => {
      try {
        if (challenge === null) {
          const result = await onStart({
            username: value.username,
            password: value.password,
          });
          // The password has done its work; keeping it in the form would leave
          // a live credential on screen behind the challenge step.
          form.setFieldValue("password", "");
          if (result.status === "challenge") onChallenge(result);
          else onClose();
          return;
        }
        const result = await onVerify({
          challenge: challenge.challenge ?? "",
          method,
          code: value.code,
        });
        if (result.status === "valid") onClose();
        else onChallenge(result);
      } catch (error) {
        form.setFieldValue("password", "");
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

  return (
    <form.AppForm>
      <DialogHeader>
        <DialogTitle>
          <Trans id="admin.federation.operator.sign-in-title">
            Sign in to VRChat
          </Trans>
        </DialogTitle>
      </DialogHeader>
      <form.Form
        label={t({
          id: "admin.federation.operator.form",
          message: "VRChat sign-in",
        })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <DialogPanel className="flex flex-col gap-4">
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
                    autoFocus
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
                  />
                )}
              </form.AppField>
            </>
          ) : (
            <>
              <div className="flex flex-col gap-2">
                <span id={methodLabelId} className="font-medium text-sm">
                  <Trans id="admin.federation.operator.method">
                    Verification method
                  </Trans>
                </span>
                <RadioGroup
                  aria-labelledby={methodLabelId}
                  value={method}
                  onValueChange={(next) => setChosen(String(next))}
                >
                  {methods.map((candidate) => (
                    <Label
                      key={candidate}
                      className="items-center gap-2 font-normal text-sm"
                    >
                      <Radio value={candidate} />
                      <MethodName method={candidate} />
                    </Label>
                  ))}
                </RadioGroup>
              </div>

              {method === "otp" ? (
                <form.AppField name="code">
                  {(field) => (
                    <field.FormField
                      label={
                        <Trans id="admin.federation.operator.code">Code</Trans>
                      }
                      autoComplete="one-time-code"
                    />
                  )}
                </form.AppField>
              ) : (
                <form.AppField name="code">
                  {(field) => (
                    <field.OtpField
                      digits={6}
                      label={
                        <Trans id="admin.federation.operator.code">Code</Trans>
                      }
                    />
                  )}
                </form.AppField>
              )}
            </>
          )}

          {busy && (
            <div className="flex items-center gap-2 text-muted-foreground text-xs">
              <Spinner aria-hidden="true" className="size-3.5" />
              <Trans id="admin.federation.operator.pending">
                Waiting for VRChat…
              </Trans>
            </div>
          )}
        </DialogPanel>
        <DialogFooter>
          <Button
            variant="ghost"
            pending={false}
            disabled={busy}
            onClick={onClose}
          >
            <Trans id="admin.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton fullWidth>
            {challenge === null ? (
              <Trans id="admin.federation.operator.submit">Sign in</Trans>
            ) : (
              <Trans id="admin.federation.operator.verify">Verify</Trans>
            )}
          </form.SubmitButton>
        </DialogFooter>
      </form.Form>
    </form.AppForm>
  );
}

function MethodName({ method }: { method: string }) {
  switch (method) {
    case "totp":
      return (
        <Trans id="admin.federation.operator.method.totp">
          Authenticator app
        </Trans>
      );
    case "emailOtp":
      return (
        <Trans id="admin.federation.operator.method.email">Email code</Trans>
      );
    default:
      return <Trans id="admin.federation.operator.method.otp">Code</Trans>;
  }
}
