import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { samlConsentRequestQueryOptions } from "@/api/queries";
import { loadConsentRequest } from "@/app/consent-loader";
import { optionalSearchText } from "@/app/search-params";
import { SamlConsentPage } from "@/pages/public/SamlConsent";

/** The SAML endpoint sends the browser here the first time a service is used. */
export const Route = createFileRoute("/_public/saml-consent")({
  validateSearch: z.object({ ticket: optionalSearchText() }),
  loaderDeps: ({ search: { ticket } }) => ({ ticket }),
  loader: ({ context, location, deps: { ticket } }) =>
    loadConsentRequest(context, location.href, ticket, (ticket) =>
      context.queryClient.query(samlConsentRequestQueryOptions(ticket)),
    ),
  component: SamlConsentPage,
});
