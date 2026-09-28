import { createFileRoute } from "@tanstack/react-router";
import { sessionQueryOptions } from "@/api/queries";
import { textParams } from "@/app/search";
import { ErrorLandingPage } from "@/pages/public/ErrorLanding";

/**
 * Where the server sends the browser when a sign-in, an OIDC authorization or
 * a SAML request cannot go on. Every value is optional and written by whoever
 * built the link, so the page only describes what it recognises.
 */
export const Route = createFileRoute("/_public/error")({
  validateSearch: (search: Record<string, unknown>) =>
    textParams(search, [
      "error",
      "reason",
      "app",
      "federationName",
      "ref",
      "return_to",
    ]),
  // The way out depends on whether anyone is signed in.
  loader: ({ context: { queryClient } }) =>
    queryClient.ensureQueryData(sessionQueryOptions()),
  component: ErrorLandingPage,
});
