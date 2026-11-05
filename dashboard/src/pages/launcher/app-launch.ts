import { useMutation } from "@tanstack/react-query";
import { useAtomValue } from "jotai";
import type { MouseEvent } from "react";
import { samlLaunchMutationOptions } from "@/api/mutations";
import { loadDocument } from "@/app/load-document";
import { browserAccountCountAtom } from "@/app/tab-account";
import { store } from "@/i18n";
import type { AppKind } from "@/pages/launcher/ProtocolCapsule";

/** The service an IdP-initiated launch address names. */
function spOf(launchUrl: string): string | undefined {
  try {
    return (
      new URL(launchUrl, window.location.origin).searchParams.get("sp") ??
      undefined
    );
  } catch {
    return undefined;
  }
}

/**
 * Opens a SAML service as the account of the tab it was opened from. Its
 * launch address is a page load, which names no account, so with several
 * accounts on the browser a press asks the server first for an address bound
 * to this tab's account, then loads that in the new tab. The tab is opened
 * at the press, before the request, because a browser only lets a page open
 * one while it is handling the press. A press with a modifier, and every
 * other kind of application, keeps the link's own behaviour.
 */
export function useAppLaunch() {
  const launch = useMutation(samlLaunchMutationOptions());
  const several = useAtomValue(browserAccountCountAtom, { store }) > 1;
  return (launchUrl: string, kind: AppKind) =>
    (event: MouseEvent<HTMLAnchorElement>) => {
      if (kind !== "saml" || !several) return;
      if (
        event.defaultPrevented ||
        event.button !== 0 ||
        event.metaKey ||
        event.ctrlKey ||
        event.shiftKey ||
        event.altKey
      ) {
        return;
      }
      const sp = spOf(launchUrl);
      if (sp === undefined) return;
      event.preventDefault();
      const tab = window.open("about:blank", "_blank");
      if (tab) tab.opener = null;
      launch.mutateAsync(sp).then(
        ({ redirect }) => {
          if (tab) tab.location.href = redirect;
          else loadDocument(redirect);
        },
        () => tab?.close(),
      );
    };
}
