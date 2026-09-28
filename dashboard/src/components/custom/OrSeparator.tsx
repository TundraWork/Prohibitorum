import { Separator } from "@heroui/react";
import { Trans } from "@lingui/react/macro";

/**
 * The rule with "or" between two ways of doing the same thing: a passkey and
 * a password in the identity check, the local sign-ins and the providers on
 * the sign-in and enrollment pages.
 */
export function OrSeparator() {
  // React Aria's separator keeps only its own attributes, so the rules are
  // hidden from the wrapper around each one.
  return (
    <div className="flex items-center gap-3">
      <div aria-hidden="true" className="flex-1">
        <Separator />
      </div>
      <span className="text-xs text-muted">
        <Trans id="common.or">or</Trans>
      </span>
      <div aria-hidden="true" className="flex-1">
        <Separator />
      </div>
    </div>
  );
}
