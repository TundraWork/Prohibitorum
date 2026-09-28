import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { sessionQueryOptions } from "@/api/queries";
import { optionalSearchText } from "@/app/search-params";
import { ErrorLandingPage } from "@/pages/public/ErrorLanding";

/**
 * Where the server sends the browser when a sign-in, an OIDC authorization or
 * a SAML request cannot go on. Every value is optional and written by whoever
 * built the link, so the page only describes what it recognises.
 */
export const Route = createFileRoute("/_public/error")({
  validateSearch: z.object({
    error: optionalSearchText(),
    reason: optionalSearchText(),
    app: optionalSearchText(),
    federationName: optionalSearchText(),
    ref: optionalSearchText(),
    return_to: optionalSearchText(),
  }),
  // The way out depends on whether anyone is signed in.
  loader: ({ context: { queryClient } }) =>
    queryClient.ensureQueryData(sessionQueryOptions()),
  component: ErrorLandingPage,
});
