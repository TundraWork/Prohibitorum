import { Trans } from "@lingui/react/macro";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { forwardAuthAppQueryOptions, sessionQueryOptions } from "@/api/queries";
import { AppAccessPanel } from "@/components/custom/AppAccessPanel";
import { Section } from "@/components/custom/Section";
import { ForwardAuthDangerSection } from "@/pages/admin/forward-auth-apps/ForwardAuthDangerSection";
import {
  ForwardAuthGeneralSection,
  ForwardAuthIconSection,
} from "@/pages/admin/forward-auth-apps/ForwardAuthGeneralSection";
import { ForwardAuthIdentitySection } from "@/pages/admin/forward-auth-apps/ForwardAuthIdentitySection";
import { ForwardAuthProxySection } from "@/pages/admin/forward-auth-apps/ForwardAuthProxySection";
import { Route } from "@/routes/_protected.admin.forward-auth-apps_.$clientId";

/**
 * One forward-auth application, as a column of sections rather than tabs.
 *
 * The whole page is on screen at once, so every section mounts with it: a block
 * that is slow or broken says so in its own place instead of taking the page
 * down, and nothing is hidden behind a control the reader has to work through.
 * That is also why the general form and the icon card are separate sections
 * rather than one — each is a short block a reader takes in alongside the
 * others, and a section's heading sits beside the button it names.
 *
 * Each panel draws its own `Section`, so a heading and the state it names stay
 * in one file. This file owns only the order and the gaps between them.
 *
 * The session is read with `useQuery` rather than suspending: the page's shape
 * does not depend on the answer, only on which controls the reader is offered,
 * and a blank section while a role resolves would be worse than a moment
 * without the administrators' one.
 */
export function AdminForwardAuthApp() {
  const { clientId } = Route.useParams();
  const { data: app } = useSuspenseQuery(forwardAuthAppQueryOptions(clientId));
  const session = useQuery(sessionQueryOptions());

  return (
    <div className="flex flex-col gap-8">
      <ForwardAuthGeneralSection app={app} />
      <ForwardAuthIconSection app={app} />
      <ForwardAuthIdentitySection app={app} />
      <ForwardAuthProxySection app={app} />
      <Section
        title={<Trans id="admin.forward-auth-apps.access">Access</Trans>}
      >
        <AppAccessPanel
          kind="forward_auth"
          appId={app.clientId}
          isAdmin={session.data?.role === "admin"}
        />
      </Section>
      <ForwardAuthDangerSection app={app} />
    </div>
  );
}
