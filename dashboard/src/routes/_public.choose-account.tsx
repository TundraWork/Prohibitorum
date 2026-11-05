import { Trans } from "@lingui/react/macro";
import { createFileRoute } from "@tanstack/react-router";
import { z } from "zod";
import { ApiError } from "@/api/errors";
import {
  accountChooserQueryOptions,
  browserAccountsQueryOptions,
} from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { sitePathOf } from "@/app/redirect";
import { optionalSearchText } from "@/app/search-params";
import { RouteError } from "@/components/custom/RouteFeedback";
import { ChooseAccountPage } from "@/pages/public/ChooseAccount";
import { ConsentExpired } from "@/pages/public/consent/ConsentExpired";

function chooserExpired(error: unknown): boolean {
  return error instanceof ApiError && error.code === "account_chooser_expired";
}

/**
 * Choosing which of the browser's accounts to continue with. With `ticket`, a
 * protocol sign-in (OIDC, SAML, forward-auth) asks, and the choice goes back
 * to the server. With `return_to`, a console address whose `u` matches
 * several accounts asks, and the choice opens that address as the account.
 */
export const Route = createFileRoute("/_public/choose-account")({
  validateSearch: z.object({
    ticket: optionalSearchText(),
    return_to: optionalSearchText(),
  }),
  loaderDeps: ({ search: { ticket, return_to } }) => ({ ticket, return_to }),
  loader: async ({ context: { queryClient }, deps }) => {
    if (deps.ticket) {
      // A ticket that expired or an account that signed out while the page
      // was away must not be served from cache.
      await queryClient.fetchQuery(
        quietQuery(accountChooserQueryOptions(deps.ticket)),
      );
      return;
    }
    if (sitePathOf(deps.return_to) === undefined) {
      throw new ApiError({ kind: "local", code: "invalid_return_to" });
    }
    await queryClient.ensureQueryData(browserAccountsQueryOptions());
  },
  // A chooser that ran out is the sign-in needing to start again from the
  // application, as an expired consent is.
  errorComponent: (props) =>
    chooserExpired(props.error) ? (
      <ConsentExpired
        retryTo=""
        message={
          <Trans id="choose_account.expired">
            This account choice has expired. Go back to the app and sign in
            again.
          </Trans>
        }
      />
    ) : (
      <RouteError {...props} />
    ),
  component: ChooseAccountPage,
});
