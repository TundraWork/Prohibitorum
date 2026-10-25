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
      title={
        <Trans id="vrchat_proof.title">VRChat profile verification link</Trans>
      }
    >
      <div className="flex flex-col gap-3 text-sm">
        <p>
          <Trans id="vrchat_proof.passing_by">
            This link proves ownership of a VRChat profile to {instance}. The
            profile's owner added it to their bio. If you're just passing by, no
            action is needed. This page doesn't sign anyone in or grant any
            access.
          </Trans>
        </p>
        <p>
          <Trans id="vrchat_proof.owner">
            If this is your profile, return to {instance} and select Verify.
            After verification, remove the link from your bio.
          </Trans>
        </p>
      </div>
    </PublicStep>
  );
}
