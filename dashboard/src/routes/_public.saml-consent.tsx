import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { samlConsentRequestQueryOptions } from "@/api/queries";
import { loadConsentRequest, startConsentReads } from "@/app/consent-loader";
import { optionalSearchText } from "@/app/search-params";
import { SamlConsentPage } from "@/pages/public/SamlConsent";

/** The SAML endpoint sends the browser here the first time a service is used. */
export const Route = createFileRoute("/_public/saml-consent")({
  validateSearch: z.object({ ticket: optionalSearchText() }),
  loaderDeps: ({ search: { ticket } }) => ({ ticket }),
  staticData: {
    startReads: ({ queryClient, search }) =>
      startConsentReads(
        queryClient,
        (search as { ticket?: string }).ticket,
        samlConsentRequestQueryOptions,
      ),
  },
  loader: ({ context, location, route, deps: { ticket } }) =>
    loadConsentRequest(
      context,
      route.id,
      location.href,
      ticket,
      samlConsentRequestQueryOptions,
    ),
  component: SamlConsentPage,
});
