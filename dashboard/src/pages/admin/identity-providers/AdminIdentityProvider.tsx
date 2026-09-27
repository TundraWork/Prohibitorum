import { Trans } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { identityProviderQueryOptions } from "@/api/queries";
import { Section } from "@/components/custom/Section";
import { ProviderClaimsSection } from "@/pages/admin/identity-providers/ProviderClaimsSection";
import { ProviderConnectionSection } from "@/pages/admin/identity-providers/ProviderConnectionSection";
import { ProviderDangerSection } from "@/pages/admin/identity-providers/ProviderDangerSection";
import { ProviderDiagnosticsSection } from "@/pages/admin/identity-providers/ProviderDiagnosticsSection";
import {
  ProviderGeneralSection,
  ProviderIconSection,
} from "@/pages/admin/identity-providers/ProviderGeneralSection";
import { ProviderOperatorSection } from "@/pages/admin/identity-providers/ProviderOperatorSection";

/**
 * One identity provider, as a column of sections rather than tabs.
 *
 * The order is the order a provider is connected. Who it is and what it does
 * with someone new comes first, with its icon, because both apply to every
 * protocol. Then, for OIDC, where it signs in and how this instance talks to
 * it, which people it may bring in and how their claims map, and whether that
 * all works. A VRChat provider has its operator session instead. The danger
 * zone is last.
 *
 * Only the sections the provider's protocol can fill are drawn. A Steam
 * provider has a name, an icon and a danger list; an empty "Connection" heading
 * over them would say a setting exists that does not.
 *
 * This file owns the order and the gaps. Each section draws its own heading and
 * saves the record through `identityProviderUpdateBody` or its own endpoint, so
 * an unsaved edit in one never leaves with a save in another. The one piece of
 * state the page holds is the diagnostic run id, because it arrives on the URL.
 */
export function AdminIdentityProvider({
  slug,
  runId,
  onRunIdChange,
}: {
  slug: string;
  /** The diagnostic run named by `?test=`, read from the route's search. */
  runId: string | undefined;
  onRunIdChange: (runId: string | undefined) => void;
}) {
  const { data: provider } = useSuspenseQuery(
    identityProviderQueryOptions(slug),
  );

  return (
    <div className="flex flex-col gap-8">
      <ProviderGeneralSection provider={provider} />

      <ProviderIconSection provider={provider} />

      {provider.protocol === "oidc" && (
        <>
          <ProviderConnectionSection provider={provider} />
          <ProviderClaimsSection provider={provider} />
          <ProviderDiagnosticsSection
            slug={provider.slug}
            runId={runId}
            onRunIdChange={onRunIdChange}
          />
        </>
      )}

      {provider.supportsOperator && (
        <Section
          title={
            <Trans id="admin.federation.operator.title">Operator session</Trans>
          }
        >
          <ProviderOperatorSection provider={provider} />
        </Section>
      )}

      <ProviderDangerSection provider={provider} />
    </div>
  );
}
