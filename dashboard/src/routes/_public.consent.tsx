import { createFileRoute } from "@tanstack/react-router";
import { consentRequestQueryOptions } from "@/api/queries";
import { loadConsentRequest } from "@/app/consent-loader";
import { textParams } from "@/app/search";
import { ConsentPage } from "@/pages/public/Consent";

/**
 * The OIDC authorization endpoint sends the browser here when an application
 * asks for consent: `ticket` names the request, and `return_to` is the
 * authorization address to resume once it is allowed.
 */
export const Route = createFileRoute("/_public/consent")({
  validateSearch: (search: Record<string, unknown>) =>
    textParams(search, ["ticket", "return_to"]),
  loaderDeps: ({ search: { ticket, return_to } }) => ({ ticket, return_to }),
  // Without the authorization to resume, an answer would have nowhere to go,
  // so a link missing it is as unusable as one missing its ticket.
  loader: ({ context, location, deps: { ticket, return_to } }) =>
    loadConsentRequest(
      context,
      location.href,
      return_to ? ticket : undefined,
      (ticket) => context.queryClient.query(consentRequestQueryOptions(ticket)),
    ),
  component: ConsentPage,
});
