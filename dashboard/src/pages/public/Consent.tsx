import { Link, linkVariants } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useSuspenseQuery } from "@tanstack/react-query";
import { Link as RouterLink, useRouter } from "@tanstack/react-router";
import { consentDecisionMutationOptions } from "@/api/mutations";
import { consentRequestQueryOptions } from "@/api/queries";
import { followRedirect } from "@/app/redirect";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import {
  type ConsentFact,
  ConsentRequest,
} from "@/pages/public/consent/ConsentRequest";
import { describeScope } from "@/pages/public/consent/scopes";
import { Route } from "@/routes/_public.consent";

/**
 * Whether to let an OIDC application in. The first time it lists everything
 * the application asked for; when the account has allowed some of it before,
 * it lists only what is new and names the rest in one line.
 */
export function ConsentPage() {
  const { ticket = "", return_to: returnTo = "" } = Route.useSearch();
  const { data: request } = useSuspenseQuery(
    consentRequestQueryOptions(ticket),
  );
  const { i18n } = useLingui();
  const { name: instance } = useInstanceBranding();
  const router = useRouter();
  const decision = useMutation(consentDecisionMutationOptions());

  const app = request.client.displayName;
  const granted = request.alreadyGranted ?? [];
  const incremental = granted.length > 0;
  const asked = incremental
    ? request.scopes.filter((scope) => !granted.includes(scope))
    : request.scopes;
  const scopeName = (scope: string) => {
    const { name } = describeScope(scope);
    return name ? i18n._(name) : scope;
  };
  const facts: ConsentFact[] = asked.map((scope) => {
    const { icon, name, description } = describeScope(scope);
    return {
      key: scope,
      icon,
      name: name ? i18n._(name) : <span className="font-mono">{scope}</span>,
      description: i18n._({ ...description, values: { instance } }),
    };
  });
  const allowed = new Intl.ListFormat(i18n.locale === "zh" ? "zh-CN" : "en", {
    type: "conjunction",
  }).format(granted.map(scopeName));
  const { policyUri, tosUri } = request.client;

  return (
    <ConsentRequest
      app={request.client}
      title={
        incremental ? (
          <Trans id="consent.title.more">{app} is asking for more access</Trans>
        ) : (
          <Trans id="consent.title">Allow {app} to access your account?</Trans>
        )
      }
      account={request.account}
      factsTitle={<Trans id="consent.facts">{app} will be able to:</Trans>}
      facts={facts}
      factsNote={
        incremental && (
          <Trans id="consent.already_granted">Already allowed: {allowed}</Trans>
        )
      }
      footnote={
        <>
          <p>
            <Trans id="consent.footnote">
              Once you allow it, {app} can sign you in directly. You can revoke
              this at any time in{" "}
              <RouterLink to="/apps" className={linkVariants().base()}>
                Connected applications
              </RouterLink>
              .
            </Trans>
          </p>
          {(policyUri || tosUri) && (
            <p>
              {policyUri && (
                <Link
                  className="text-sm"
                  href={policyUri}
                  target="_blank"
                  rel="noreferrer"
                >
                  <Trans id="consent.policy">{app}'s privacy policy</Trans>
                </Link>
              )}
              {policyUri && tosUri && <span aria-hidden="true"> · </span>}
              {tosUri && (
                <Link
                  className="text-sm"
                  href={tosUri}
                  target="_blank"
                  rel="noreferrer"
                >
                  {policyUri ? (
                    <Trans id="consent.terms">Terms of service</Trans>
                  ) : (
                    <Trans id="consent.terms.app">
                      {app}'s terms of service
                    </Trans>
                  )}
                </Link>
              )}
            </p>
          )}
        </>
      }
      decline={<Trans id="consent.deny">Deny</Trans>}
      approve={<Trans id="consent.allow">Allow</Trans>}
      onDecide={async (choice) => {
        const { redirect } = await decision.mutateAsync({
          ticket,
          returnTo,
          decision: choice === "approve" ? "approve" : "deny",
        });
        // Allowing resumes the authorization on the server; denying goes to
        // the application's callback. Both leave the console.
        await followRedirect(router, redirect);
      }}
      // The authorization is resumed from the start, so the next account is
      // asked about it afresh; the ticket belongs to this one.
      switchAccountTo={returnTo}
    />
  );
}
