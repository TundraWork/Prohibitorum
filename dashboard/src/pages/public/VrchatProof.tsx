import { Trans } from "@lingui/react/macro";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";

/**
 * What a verification link in a VRChat bio is, for whoever follows it. It
 * does nothing and asks for nothing: the proof in the address is not read,
 * so the page cannot sign anyone in, and it is never written on the page.
 */
export function VrchatProofPage() {
  const { name: instance } = useInstanceBranding();
  return (
    <PublicStep
      title={<Trans id="vrchat_proof.title">This is a verification link</Trans>}
    >
      <div className="flex flex-col gap-3 text-sm">
        <p>
          <Trans id="vrchat_proof.passing_by">
            Someone put this link in their VRChat bio to prove to {instance}{" "}
            that the profile is theirs. If you're just passing by, there's
            nothing to do: this page doesn't sign anyone in or grant any access.
          </Trans>
        </p>
        <p>
          <Trans id="vrchat_proof.owner">
            If this is your profile, go back to {instance} and press Verify.
            Once it's verified, remove the link from your bio.
          </Trans>
        </p>
      </div>
    </PublicStep>
  );
}
