import { Trans } from "@lingui/react/macro";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { oidcAppQueryOptions, sessionQueryOptions } from "@/api/queries";
import { AppAccessPanel } from "@/components/custom/AppAccessPanel";
import { Section } from "@/components/custom/Section";
import { DangerSection } from "@/pages/admin/oidc-applications/DangerSection";
import {
  GeneralSection,
  OidcIconSection,
} from "@/pages/admin/oidc-applications/GeneralSection";
import { IdentityProjectionSection } from "@/pages/admin/oidc-applications/IdentityProjectionSection";
import { Route } from "@/routes/_protected.admin.oidc-applications_.$clientId";

/**
 * One OIDC application's settings, as a column of sections rather than tabs.
 *
 * The page is short enough to take in at once — four blocks, each a small form
 * or a list — and a reader changing a redirect URI usually also wants to check
 * the projection they wrote against it. So every section is mounted on arrival
 * and the page has no control to work through; the cost is that every section's
 * reads start at once, which is why each also brings its own boundary and says
 * where a failure is (`AGENTS.md`, "Console layout").
 *
 * The sections are independent in a second sense: they do not share unsaved
 * state. Each saves the record it edits through `oidcAppUpdateBody`, so leaving
 * the page abandons whatever was typed in a form that was never submitted,
 * exactly as on the account's own pages.
 *
 * This file owns the order and the gaps. Each section draws its own heading, so
 * a section's action stays beside the state it reads, and the Client ID is
 * passed down rather than read again: the route loader already has the record.
 */
export function AdminOidcApplication() {
  const { clientId } = Route.useParams();
  const { data: app } = useSuspenseQuery(oidcAppQueryOptions(clientId));
  // Read with `useQuery` rather than suspending: the page's shape does not
  // depend on the answer, only on which controls the reader is offered, and a
  // blank section while a role resolves would be worse than a moment without
  // the administrators' one.
  const session = useQuery(sessionQueryOptions());

  return (
    <div className="flex flex-col gap-8">
      <GeneralSection app={app} />

      <OidcIconSection app={app} />

      <IdentityProjectionSection app={app} />

      <Section title={<Trans id="admin.oidc-apps.access">Access</Trans>}>
        <AppAccessPanel
          kind="oidc"
          appId={app.clientId}
          isAdmin={session.data?.role === "admin"}
        />
      </Section>

      <DangerSection app={app} />
    </div>
  );
}
