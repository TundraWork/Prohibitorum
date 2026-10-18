import { Trans } from "@lingui/react/macro";
import type { ReactNode } from "react";

/**
 * The rule with a label between two ways of doing the same thing — "or" by
 * default — as a passkey and a password in the identity check, or the local
 * sign-ins and the providers on the sign-in and enrollment pages.
 */
export function OrSeparator({ label }: { label?: ReactNode }) {
  return (
    <div className="flex items-center gap-3 text-muted-foreground text-xs before:h-px before:flex-1 before:bg-border after:h-px after:flex-1 after:bg-border">
      {label ?? <Trans id="common.or">or</Trans>}
    </div>
  );
}
