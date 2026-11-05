import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import {
  browserAccountsQueryOptions,
  sessionQueryOptions,
} from "@/api/queries";
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
  loaderDeps: ({ search: { reason } }) => ({
    limited: reason === "account_limit_reached",
  }),
  // The way out depends on whether anyone is signed in; a sign-in stopped
  // at the account limit lists the accounts.
  loader: ({ context: { queryClient }, deps }) =>
    Promise.all([
      queryClient.ensureQueryData(sessionQueryOptions()),
      deps.limited
        ? queryClient.ensureQueryData(browserAccountsQueryOptions())
        : undefined,
    ]),
  component: ErrorLandingPage,
});
