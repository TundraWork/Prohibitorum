import {
  SecretReveal,
  SecretRevealDialog,
} from "@/components/custom/SecretReveal";
import {
  firstRecoveryCodesCopy,
  recoveryCodesCopy,
} from "@/components/custom/secret-reveal-copy";

/**
 * Recovery codes the server has just issued. `firstIssue` is for the first
 * codes an account gets, whose wording has no earlier codes to replace;
 * `dialog` shows them in a dialog that cannot be dismissed before they are
 * saved.
 */
export function RecoveryCodes({
  codes,
  onContinue,
  dialog = false,
  titled,
  firstIssue = false,
}: {
  codes: string[];
  onContinue: () => Promise<void>;
  dialog?: boolean;
  /** Off when the caller draws the title; see `SecretReveal`. */
  titled?: boolean;
  firstIssue?: boolean;
}) {
  const props = {
    text: codes.join("\n"),
    filename: "prohibitorum-recovery-codes.txt",
    copy: firstIssue ? firstRecoveryCodesCopy : recoveryCodesCopy,
    onContinue,
  };
  return dialog ? (
    <SecretRevealDialog {...props} />
  ) : (
    <SecretReveal {...props} titled={titled} />
  );
}
