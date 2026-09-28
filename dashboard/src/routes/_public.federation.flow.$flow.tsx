import { createFileRoute } from "@tanstack/react-router";
import { federationFlowQueryOptions } from "@/api/queries";
import { FederationFlowPage } from "@/pages/public/FederationFlow";

/**
 * A VRChat profile verification, for a sign-in, a link, an invitation or a
 * new account. The flow belongs to the browser that started it, through the
 * federation cookie; one that has expired fails to load.
 */
export const Route = createFileRoute("/_public/federation/flow/$flow")({
  loader: ({ context: { queryClient }, params: { flow } }) =>
    queryClient.query(federationFlowQueryOptions(flow)),
  component: FederationFlowPage,
});
