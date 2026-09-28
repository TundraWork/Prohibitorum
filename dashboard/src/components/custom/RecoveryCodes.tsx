import { SecretReveal } from "@/components/custom/SecretReveal";
import {
  firstRecoveryCodesCopy,
  recoveryCodesCopy,
} from "@/components/custom/secret-reveal-copy";

/**
 * Recovery codes the server has just issued. `firstIssue` is for the first
 * codes an account gets, whose wording has no earlier codes to replace.
 */
export function RecoveryCodes({
  codes,
  onContinue,
  onSurface,
  inDialog,
  titled,
  firstIssue = false,
}: {
  codes: string[];
  onContinue: () => Promise<void>;
  onSurface?: boolean;
  inDialog?: boolean;
  /** Off when the caller draws the title; see `SecretReveal`. */
  titled?: boolean;
  firstIssue?: boolean;
}) {
  return (
    <SecretReveal
      text={codes.join("\n")}
      filename="prohibitorum-recovery-codes.txt"
      copy={firstIssue ? firstRecoveryCodesCopy : recoveryCodesCopy}
      onContinue={onContinue}
      onSurface={onSurface}
      inDialog={inDialog}
      titled={titled}
    />
  );
}
