import { Trans } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { logoutMutationOptions } from "@/api/mutations";
import { clearSessionQueries } from "@/api/queries";
import { loadDocument } from "@/app/load-document";
import { usePageRestore } from "@/app/use-page-restore";
import { Button } from "@/components/custom/Button";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PublicStep } from "@/components/custom/PublicStep";
import { Route } from "@/routes/_public.sign-out";

const ssoLogoutPath = "/api/prohibitorum/forward-auth/sso-logout";

/**
 * Asks before ending the session when an application's sign-out could have
 * come from anywhere. Signing out returns through sso-logout, which checks
 * `rd` and sends the browser back to the application; the page itself never
 * goes to `rd`, which anyone can write into the address.
 */
export function SignOutPage() {
  const { rd } = Route.useSearch();
  const { name } = useInstanceBranding();
  const queryClient = useQueryClient();
  const navigate = useNavigate();
  const [leaving, setLeaving] = useState(false);
  const signOut = useMutation({
    ...logoutMutationOptions(queryClient),
    onSuccess: async () => {
      await clearSessionQueries(queryClient);
      setLeaving(true);
      loadDocument(
        rd ? `${ssoLogoutPath}?rd=${encodeURIComponent(rd)}` : ssoLogoutPath,
      );
    },
  });
  usePageRestore(() => setLeaving(false));

  const cancel = async () => {
    if (window.history.length > 1) {
      window.history.back();
      return;
    }
    await navigate({ to: "/" });
  };

  const busy = signOut.isPending || leaving;
  return (
    <PublicStep
      title={<Trans id="sign-out.title">Sign out of {name}?</Trans>}
      description={
        <Trans id="sign-out.description">
          An application asked to sign you out of {name}. If you did not choose
          to sign out, cancel.
        </Trans>
      }
      actions={
        <PublicStep.Actions layout="decision">
          <Button
            disabled={busy}
            onClick={() => void cancel()}
            variant="outline"
            size="lg"
          >
            <Trans id="sign-out.cancel">Cancel</Trans>
          </Button>
          <Button pending={busy} onClick={() => signOut.mutate()} size="lg">
            <Trans id="sign-out.confirm">Sign out</Trans>
          </Button>
        </PublicStep.Actions>
      }
    />
  );
}
