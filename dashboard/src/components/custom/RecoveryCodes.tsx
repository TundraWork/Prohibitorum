import { SecretReveal } from "@/components/custom/SecretReveal";
import { recoveryCodesCopy } from "@/components/custom/secret-reveal-copy";

export function RecoveryCodes({
  codes,
  onContinue,
  onSurface,
}: {
  codes: string[];
  onContinue: () => Promise<void>;
  onSurface?: boolean;
}) {
  return (
    <SecretReveal
      text={codes.join("\n")}
      filename="prohibitorum-recovery-codes.txt"
      copy={recoveryCodesCopy}
      onContinue={onContinue}
      onSurface={onSurface}
    />
  );
}
