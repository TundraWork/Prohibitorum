import { useLingui } from "@lingui/react/macro";
import { useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { samlAppQueryOptions, sessionQueryOptions } from "@/api/queries";
import { AppAccessSection } from "@/components/custom/AppAccessSection";
import { AppDetailLayout } from "@/components/custom/AppDetailLayout";
import {
  AppManagerNote,
  AppManagersSection,
} from "@/components/custom/AppManagersSection";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { ObjectHeader } from "@/components/custom/ObjectHeader";
import { CertificateText } from "@/pages/admin/saml-applications/CertificateText";
import { SamlAppearanceSection } from "@/pages/admin/saml-applications/SamlAppearanceSection";
import { SamlDangerSection } from "@/pages/admin/saml-applications/SamlDangerSection";
import { SamlIdentitySection } from "@/pages/admin/saml-applications/SamlIdentitySection";
import { SamlMetadataSection } from "@/pages/admin/saml-applications/SamlMetadataSection";
import { SamlSignInSection } from "@/pages/admin/saml-applications/SamlSignInSection";
import { certificateState } from "@/pages/admin/saml-applications/saml-certificate";
import { Route } from "@/routes/_protected.admin.saml-applications_.$id";

/**
 * One SAML application's page: who it is at the top, then a column of sections
 * with an outline beside them.
 *
 * The order is the order a service provider is wired up. How people sign in
 * comes first, because the lifetime and the two request rules are what most
 * visits adjust. Then who may use and manage it, what the assertion tells the
 * provider about the account, what the provider's own metadata gave us, how the
 * application is shown, and last the changes that stop it.
 *
 * Every section is mounted on arrival, so its reads start at once, and each
 * saves the record it edits through `samlAppUpdateBody` or its own endpoint.
 * They do not share unsaved state: leaving the page abandons whatever was typed
 * in a form that was never submitted. The record is passed down rather than
 * read again: the route loader already has it.
 */
export function AdminSamlApplication() {
  const { t } = useLingui();
  const { id } = Route.useParams();
  const { data: app } = useSuspenseQuery(samlAppQueryOptions(Number(id)));
  // Read with `useQuery` rather than suspending: the page's shape does not
  // depend on the answer, only on which controls the reader is offered, and a
  // blank section while a role resolves would be worse than a moment without
  // the administrators' one.
  const session = useQuery(sessionQueryOptions());
  const isAdmin = session.data?.role === "admin";

  const name = app.displayName || app.entityId;
  const applicationId = String(app.id);

  return (
    <AppDetailLayout
      header={
        <div className="flex flex-col gap-3">
          <ObjectHeader
            icon={
              <EntityAvatar
                size="xl"
                iconUrl={app.iconUrl}
                name={name}
                className="rounded-2xl"
                fallbackClassName="rounded-2xl"
              />
            }
            title={name}
            statuses={[
              app.disabled ? "disabled" : "enabled",
              app.accessRestricted ? "restricted" : "open",
            ]}
            identifier={{
              value: app.entityId,
              label: t({
                id: "admin.saml-apps.copy-entity-id",
                message: "Copy the Entity ID",
              }),
            }}
            facts={[
              <CertificateText
                named
                key="certificate"
                state={certificateState(app)}
              />,
            ]}
          />
          <AppManagerNote isAdmin={isAdmin} />
        </div>
      }
    >
      <SamlSignInSection app={app} />
      <AppAccessSection kind="saml" appId={applicationId} isAdmin={isAdmin} />
      <AppManagersSection kind="saml" appId={applicationId} isAdmin={isAdmin} />
      <SamlIdentitySection app={app} />
      <SamlMetadataSection app={app} />
      <SamlAppearanceSection app={app} />
      <SamlDangerSection app={app} />
    </AppDetailLayout>
  );
}
