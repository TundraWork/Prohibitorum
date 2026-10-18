import { useLingui } from "@lingui/react/macro";
import { StepHeader } from "@/components/custom/StepHeader";

/**
 * Where the reader is in setting up a password and an authenticator: the
 * password, the authenticator, then the recovery codes the server answers with.
 * `current` is the zero-based index of the step on screen.
 */
export function PasswordTotpStepHeader({ current }: { current: number }) {
  const { t } = useLingui();
  return (
    <StepHeader
      label={t({
        id: "enroll.password_totp.steps",
        message: "Sign-in setup steps",
      })}
      current={current}
      steps={[
        t({ id: "enroll.password_totp.steps.password", message: "Password" }),
        t({
          id: "enroll.password_totp.steps.authenticator",
          message: "Authenticator",
        }),
        t({
          id: "enroll.password_totp.steps.codes",
          message: "Recovery codes",
        }),
      ]}
    />
  );
}
