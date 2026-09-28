import { createFileRoute } from "@tanstack/react-router";
import { federationConfirmQueryOptions } from "@/api/queries";
import { WelcomePage } from "@/pages/public/Welcome";

/**
 * Where a first sign-in through an upstream provider lands: the account it
 * prepared, to confirm before the session is issued. The prepared sign-in
 * lives in the browser's federation cookie; once it has expired the page
 * fails to load and says so.
 */
export const Route = createFileRoute("/_public/welcome")({
  loader: ({ context: { queryClient } }) =>
    queryClient.query(federationConfirmQueryOptions()),
  component: WelcomePage,
});
