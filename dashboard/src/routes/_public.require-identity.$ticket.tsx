import { createFileRoute } from "@tanstack/react-router";
import { requireIdentityRequestQueryOptions } from "@/api/queries";
import { loadConsentRequest, startConsentReads } from "@/app/consent-loader";
import { RequireIdentityPage } from "@/pages/public/RequireIdentity";

/**
 * An application's require-identity request sends the browser here when the
 * account has no identity at any of the providers it listed. The request is
 * the session's own, so without one the page goes to sign in and comes back.
 */
export const Route = createFileRoute("/_public/require-identity/$ticket")({
  staticData: {
    startReads: ({ queryClient, params }) =>
      startConsentReads(
        queryClient,
        params.ticket,
        requireIdentityRequestQueryOptions,
      ),
  },
  loader: ({ context, location, route, params: { ticket } }) =>
    loadConsentRequest(
      context,
      route.id,
      location.href,
      ticket,
      requireIdentityRequestQueryOptions,
    ),
  component: RequireIdentityPage,
});
