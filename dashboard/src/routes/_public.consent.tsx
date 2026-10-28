import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { consentRequestQueryOptions } from "@/api/queries";
import { loadConsentRequest, startConsentReads } from "@/app/consent-loader";
import { optionalSearchText } from "@/app/search-params";
import { RouteError } from "@/components/custom/RouteFeedback";
import { ConsentPage } from "@/pages/public/Consent";
import {
  ConsentExpired,
  ConsentExpiredError,
} from "@/pages/public/consent/ConsentExpired";

const consentSearch = z.object({
  ticket: optionalSearchText(),
  return_to: optionalSearchText(),
});

// Without the authorization to resume, an answer would have nowhere to go, so
// a link missing it is as unusable as one missing its ticket.
const usableTicket = ({ ticket, return_to }: z.infer<typeof consentSearch>) =>
  return_to ? ticket : undefined;

/**
 * The OIDC authorization endpoint sends the browser here when an application
 * asks for consent: `ticket` names the request, and `return_to` is the
 * authorization address to resume once it is allowed.
 */
export const Route = createFileRoute("/_public/consent")({
  validateSearch: consentSearch,
  loaderDeps: ({ search: { ticket, return_to } }) => ({ ticket, return_to }),
  staticData: {
    startReads: ({ queryClient, search }) =>
      startConsentReads(
        queryClient,
        usableTicket(search as z.infer<typeof consentSearch>),
        consentRequestQueryOptions,
      ),
  },
  loader: ({ context, location, route, deps }) =>
    loadConsentRequest(
      context,
      route.id,
      location.href,
      usableTicket(deps),
      consentRequestQueryOptions,
      deps.return_to,
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
