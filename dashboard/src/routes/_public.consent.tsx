import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { consentRequestQueryOptions } from "@/api/queries";
import { loadConsentRequest } from "@/app/consent-loader";
import { optionalSearchText } from "@/app/search-params";
import { RouteError } from "@/components/custom/RouteFeedback";
import { ConsentPage } from "@/pages/public/Consent";
import {
  ConsentExpired,
  ConsentExpiredError,
} from "@/pages/public/consent/ConsentExpired";

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
      return_to,
    ),
  // An unusable ticket is not the page failing: it is the authorization
  // needing to be started again, which the expired card offers. Everything
  // else keeps the shared failure view.
  errorComponent: (props) =>
    props.error instanceof ConsentExpiredError ? (
      <ConsentExpired retryTo={props.error.retryTo} />
    ) : (
      <RouteError {...props} />
    ),
  component: ConsentPage,
});
