import { linkVariants } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useSuspenseQuery } from "@tanstack/react-query";
import { Link as RouterLink } from "@tanstack/react-router";
import { Tag, UserRound } from "lucide-react";
import { samlConsentDecisionMutationOptions } from "@/api/mutations";
import { samlConsentRequestQueryOptions } from "@/api/queries";
import { loadDocument } from "@/app/load-document";
import {
  type ConsentFact,
  ConsentRequest,
} from "@/pages/public/consent/ConsentRequest";
import { Route } from "@/routes/_public.saml-consent";

/**
 * Whether to continue into a SAML service for the first time. SAML asks
 * nothing of the account beyond what the service is configured to receive, so
 * the page lists the attributes it will be sent.
 */
export function SamlConsentPage() {
  const { ticket = "" } = Route.useSearch();
  const { data: request } = useSuspenseQuery(
    samlConsentRequestQueryOptions(ticket),
  );
  const { t } = useLingui();
  const decision = useMutation(samlConsentDecisionMutationOptions());

  const sp = request.sp.displayName;
  const facts: ConsentFact[] =
    request.attributes.length > 0
      ? request.attributes.map((label) => ({
          key: label,
          icon: Tag,
          name: label,
        }))
      : [
          {
            key: "basic",
            icon: UserRound,
            name: t({
              id: "saml_consent.basic",
              message: "Your basic account information",
            }),
          },
        ];

  return (
    <ConsentRequest
      app={request.sp}
      title={<Trans id="saml_consent.title">Continue to {sp}?</Trans>}
      account={request.account}
      factsTitle={<Trans id="saml_consent.facts">{sp} will receive:</Trans>}
      facts={facts}
      footnote={
        <p>
          <Trans id="saml_consent.footnote">
            You only need to confirm this once. You can revoke it at any time in{" "}
            <RouterLink to="/apps" className={linkVariants().base()}>
              Connected applications
            </RouterLink>
            .
          </Trans>
        </p>
      }
      decline={<Trans id="saml_consent.decline">Not now</Trans>}
      approve={<Trans id="saml_consent.continue">Continue</Trans>}
      onDecide={async (choice) => {
        const { redirect } = await decision.mutateAsync({
          ticket,
          decision: choice,
        });
        // Always a full load: continuing goes to the endpoint that posts the
        // assertion, and declining must not leave the SAML flow's state in
        // the page.
        loadDocument(redirect);
      }}
      // The ticket belongs to this account; another one starts the sign-in
      // from the service again.
      switchAccountTo="/"
    />
  );
}
