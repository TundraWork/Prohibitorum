import { Trans } from "@lingui/react/macro";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { samlAppQueryOptions, sessionQueryOptions } from "@/api/queries";
import { AppAccessPanel } from "@/components/custom/AppAccessPanel";
import { Section } from "@/components/custom/Section";
import { SamlDangerSection } from "@/pages/admin/saml-applications/SamlDangerSection";
import {
  SamlGeneralSection,
  SamlIconSection,
} from "@/pages/admin/saml-applications/SamlGeneralSection";
import { SamlIdentitySection } from "@/pages/admin/saml-applications/SamlIdentitySection";
import { SamlMetadataSection } from "@/pages/admin/saml-applications/SamlMetadataSection";
import { Route } from "@/routes/_protected.admin.saml-applications_.$id";

/**
 * One SAML application, as a column of sections.
 *
 * A stack rather than a tab strip, for the reason the rest of the console's
 * detail pages are: these blocks are short, a reader checks several of them in
 * one visit, and a change to the metadata is often followed by one to the
 * attribute map. Nothing is collapsed and nothing is hidden behind a control to
 * work through, so the page says what the application is at a glance.
 *
 * Every section is mounted at once, which is what the layout implies: each reads
 * what it needs on arrival and owns the write it performs. They do not share
 * unsaved state — leaving the page abandons anything not submitted, exactly as
 * on the user and provider detail pages.
 *
 * The page owns the order and the gaps and nothing else. Each section draws its
 * own `Section` and its own card, so a section's title stays beside the action
 * it names and its handler stays with the state it reads.
 */
export function AdminSamlApplication() {
  const { id } = Route.useParams();
  const { data: app } = useSuspenseQuery(samlAppQueryOptions(Number(id)));
  // Read with `useQuery` rather than suspending: the page's shape does not
  // depend on the answer, only on which controls the reader is offered, and a
  // blank section while a role resolves would be worse than a moment without
  // the administrators' one. The access panel is told, not asked — it draws the
  // manager list for an administrator and does not request it for anyone else.
  const session = useQuery(sessionQueryOptions());

  const applicationId = String(app.id);

  return (
    <div className="flex flex-col gap-8">
      <SamlGeneralSection app={app} />

      <SamlIconSection app={app} />

      <SamlIdentitySection app={app} />

      <SamlMetadataSection app={app} />

      <Section title={<Trans id="admin.saml-apps.access">Access</Trans>}>
        <AppAccessPanel
          kind="saml"
          appId={applicationId}
          isAdmin={session.data?.role === "admin"}
        />
      </Section>

      <SamlDangerSection app={app} />
    </div>
  );
}
