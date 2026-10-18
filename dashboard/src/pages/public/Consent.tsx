import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useSuspenseQuery } from "@tanstack/react-query";
import { Link as RouterLink, useRouter } from "@tanstack/react-router";
import { ChevronRightIcon } from "lucide-react";
import { useState } from "react";
import { ApiError } from "@/api/errors";
import { consentDecisionMutationOptions } from "@/api/mutations";
import { consentRequestQueryOptions } from "@/api/queries";
import { withInlineReport } from "@/app/query-client";
import { followRedirect } from "@/app/redirect";
import { inlineLinkClass } from "@/components/custom/inline-link";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { type ScopeRow, ScopeRows } from "@/components/custom/ScopeRows";
import { Badge } from "@/components/ui/badge";
import {
  Collapsible,
  CollapsiblePanel,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { ConsentExpired } from "@/pages/public/consent/ConsentExpired";
import { ConsentRequest } from "@/pages/public/consent/ConsentRequest";
import { describeScope } from "@/pages/public/consent/scopes";
import { Route } from "@/routes/_public.consent";

function ticketConsumed(error: unknown): boolean {
  return error instanceof ApiError && error.code === "invalid_consent_ticket";
}

/**
 * Whether to let an OIDC application in. The first time it lists everything
 * the application asked for; when the account has allowed some of it before,
 * it lists only what is new and folds the rest away under one heading.
 */
export function ConsentPage() {
  const { ticket = "", return_to: returnTo = "" } = Route.useSearch();
  const { data: request } = useSuspenseQuery(
    consentRequestQueryOptions(ticket),
  );
  const { i18n } = useLingui();
  const { name: instance } = useInstanceBranding();
  const router = useRouter();
  const decision = useMutation(
    withInlineReport(consentDecisionMutationOptions(), ticketConsumed),
  );
  const [expired, setExpired] = useState(false);

  // The ticket is consumed by the answer itself, so a failed answer cannot be
  // sent again: starting the authorization afresh is the one way on, offered
  // by the same card the loader's expired ticket shows.
  if (expired) return <ConsentExpired retryTo={returnTo} />;
  const app = request.client.displayName;
  const granted = request.alreadyGranted ?? [];
  const incremental = granted.length > 0;
  const asked = incremental
    ? request.scopes.filter((scope) => !granted.includes(scope))
    : request.scopes;
  const providers = request.providers ?? {};
  const rowOf = (scope: string): ScopeRow => {
    const {
      icon: Icon,
      name,
      description,
      values,
    } = describeScope(scope, providers);
    return {
      key: scope,
      icon: <Icon />,
      title: name ? (
        i18n._({ ...name, values })
      ) : (
        <span className="font-mono text-[13px] wrap-anywhere">{scope}</span>
      ),
      detail: i18n._({ ...description, values: { ...values, instance } }),
    };
  };
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
      rowsLabel={
        incremental ? (
          <>
            <Trans id="consent.new_for">New for {app}</Trans>
            <span className="sr-only">
              <Trans id="consent.count_separator">,</Trans>
            </span>{" "}
            <Badge variant="info">{asked.length}</Badge>
          </>
        ) : (
          <Trans id="consent.facts">{app} will be able to</Trans>
        )
      }
      rows={asked.map(rowOf)}
      after={
        incremental && (
          <Collapsible className="mt-1">
            <CollapsibleTrigger
              id="consent-allowed"
              className="flex w-full items-center gap-2 rounded-md py-1.5 font-medium text-muted-foreground text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring data-panel-open:[&_svg]:rotate-90"
            >
              <ChevronRightIcon
                aria-hidden="true"
                className="size-4 transition-transform"
              />
              <Trans id="consent.already_allowed">Already allowed</Trans>
              <span className="sr-only">
                <Trans id="consent.count_separator">,</Trans>
              </span>{" "}
              <Badge variant="outline">{granted.length}</Badge>
            </CollapsibleTrigger>
            <CollapsiblePanel>
              <div className="pt-2">
                <ScopeRows
                  rows={granted.map(rowOf)}
                  labelledBy="consent-allowed"
                />
              </div>
            </CollapsiblePanel>
          </Collapsible>
        )
      }
      footnote={
        <>
          <Trans id="consent.footnote">
            Once you allow it, {app} can sign you in directly. You can revoke
            this at any time in{" "}
            <RouterLink to="/apps" className={inlineLinkClass}>
              Connected applications
            </RouterLink>
            .
          </Trans>
          {(policyUri || tosUri) && (
            <span className="mt-1 block">
              {policyUri && (
                <a
                  className={inlineLinkClass}
                  href={policyUri}
                  target="_blank"
                  rel="noreferrer"
                >
                  <Trans id="consent.policy">{app}'s privacy policy</Trans>
                </a>
              )}
              {policyUri && tosUri && <span aria-hidden="true"> · </span>}
              {tosUri && (
                <a
                  className={inlineLinkClass}
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
                </a>
              )}
            </span>
          )}
        </>
      }
      decline={<Trans id="consent.deny">Deny</Trans>}
      approve={<Trans id="consent.allow">Allow</Trans>}
      onDecide={async (choice) => {
        try {
          const { redirect } = await decision.mutateAsync({
            ticket,
            returnTo,
            decision: choice === "approve" ? "approve" : "deny",
          });
          // Allowing resumes the authorization on the server; denying goes to
          // the application's callback. Both leave the console.
          await followRedirect(router, redirect);
        } catch (error) {
          // The ticket is single-use and was consumed by this answer, so
          // sending it again can only fail — offer the restart instead.
          if (ticketConsumed(error)) {
            setExpired(true);
            return;
          }
          throw error;
        }
      }}
      // The authorization is resumed from the start, so the next account is
      // asked about it afresh; the ticket belongs to this one.
      switchAccountTo={returnTo}
    />
  );
}
