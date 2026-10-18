import type { ReactNode } from "react";
import {
  SecretReveal,
  SecretRevealDialog,
  SecretRevealStep,
} from "@/components/custom/SecretReveal";
import {
  firstRecoveryCodesCopy,
  recoveryCodesCopy,
} from "@/components/custom/secret-reveal-copy";

/**
 * Recovery codes the server has just issued. `firstIssue` is for the first
 * codes an account gets, whose wording has no earlier codes to replace;
 * `dialog` shows them in a dialog that cannot be dismissed before they are
 * saved. `progress` instead draws them as the last step of a dialog the caller
 * owns, with that step header above the title.
 */
export function RecoveryCodes({
  codes,
  onContinue,
  dialog = false,
  titled,
  firstIssue = false,
  progress,
}: {
  codes: string[];
  onContinue: () => Promise<void>;
  dialog?: boolean;
  /** Off when the caller draws the title; see `SecretReveal`. */
  titled?: boolean;
  firstIssue?: boolean;
  progress?: ReactNode;
}) {
  const props = {
    text: codes.join("\n"),
    filename: "prohibitorum-recovery-codes.txt",
    copy: firstIssue ? firstRecoveryCodesCopy : recoveryCodesCopy,
    onContinue,
  };
  if (progress !== undefined) {
    return <SecretRevealStep {...props} progress={progress} />;
  }
  return dialog ? (
    <SecretRevealDialog {...props} />
  ) : (
    <SecretReveal {...props} titled={titled} />
  );
}
