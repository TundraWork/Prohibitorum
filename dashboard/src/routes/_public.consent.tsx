import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { consentRequestQueryOptions } from "@/api/queries";
import { loadConsentRequest } from "@/app/consent-loader";
import { optionalSearchText } from "@/app/search-params";
import { ConsentPage } from "@/pages/public/Consent";

/**
 * The OIDC authorization endpoint sends the browser here when an application
 * asks for consent: `ticket` names the request, and `return_to` is the
 * authorization address to resume once it is allowed.
 */
export const Route = createFileRoute("/_public/consent")({
  validateSearch: z.object({
    ticket: optionalSearchText(),
    return_to: optionalSearchText(),
  }),
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
