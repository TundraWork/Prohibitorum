import { createFileRoute } from "@tanstack/react-router";
import { samlConsentRequestQueryOptions } from "@/api/queries";
import { loadConsentRequest } from "@/app/consent-loader";
import { textParams } from "@/app/search";
import { SamlConsentPage } from "@/pages/public/SamlConsent";

/** The SAML endpoint sends the browser here the first time a service is used. */
export const Route = createFileRoute("/_public/saml-consent")({
  validateSearch: (search: Record<string, unknown>) =>
    textParams(search, ["ticket"]),
  loaderDeps: ({ search: { ticket } }) => ({ ticket }),
  loader: ({ context, location, deps: { ticket } }) =>
    loadConsentRequest(context, location.href, ticket, (ticket) =>
      context.queryClient.query(samlConsentRequestQueryOptions(ticket)),
    ),
  component: SamlConsentPage,
});
