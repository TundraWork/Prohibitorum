import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { TriangleAlertIcon, UserRoundCogIcon } from "lucide-react";
import { readProviderProtocol } from "@/api/federation";
import { identityProviderQueryOptions } from "@/api/queries";
import { AppDetailLayout } from "@/components/custom/AppDetailLayout";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { ObjectHeader } from "@/components/custom/ObjectHeader";
import { Section } from "@/components/custom/Section";
import { ProviderAppearanceSection } from "@/pages/admin/identity-providers/ProviderAppearanceSection";
import { ProviderClaimsSection } from "@/pages/admin/identity-providers/ProviderClaimsSection";
import { ProviderConnectionSection } from "@/pages/admin/identity-providers/ProviderConnectionSection";
import { ProviderCredentialsSection } from "@/pages/admin/identity-providers/ProviderCredentialsSection";
import { ProviderDangerSection } from "@/pages/admin/identity-providers/ProviderDangerSection";
import { ProviderDiagnosticsSection } from "@/pages/admin/identity-providers/ProviderDiagnosticsSection";
import { ProviderGeneralSection } from "@/pages/admin/identity-providers/ProviderGeneralSection";
import { ProviderOperatorSection } from "@/pages/admin/identity-providers/ProviderOperatorSection";
import { providerProtocolLabel } from "@/pages/admin/identity-providers/provider-options";
import { notReadyReason } from "@/pages/admin/identity-providers/provider-readiness";

/**
 * One identity provider's page: who it is at the top, then a column of sections
 * with an outline beside them.
 *
 * The order is the order a provider is connected. What it does with someone
 * who signs in comes first, because it applies to every protocol. Then, for
 * OIDC, where it signs in and how this instance talks to it, which people it
 * may bring in and how their claims map, and whether that all works; a Steam
 * provider has its Web API key and a VRChat provider its operator session
 * instead. How the provider is shown comes next, and last the changes that
 * stop it.
 *
 * Only the sections the provider's protocol can fill are drawn: an empty
 * "Connection" heading over a Steam provider would say a setting exists that
 * does not.
 *
 * This file owns the order. Each section saves the record through
 * `identityProviderUpdateBody` or its own endpoint, so an unsaved edit in one
 * never leaves with a save in another. The one piece of state the page holds is
 * the diagnostic run id, because it arrives on the URL.
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
  const { t, i18n } = useLingui();
  const { data: provider } = useSuspenseQuery(
    identityProviderQueryOptions(slug),
  );

  const name = provider.displayName || provider.slug;
  const protocol = readProviderProtocol(provider.protocol);

  return (
    <AppDetailLayout
      header={
        <div className="flex flex-col gap-3">
          <ObjectHeader
            icon={
              <EntityAvatar
                size="xl"
                iconUrl={provider.iconUrl}
                name={name}
                className="rounded-2xl"
                fallbackClassName="rounded-2xl"
              />
            }
            title={name}
            statuses={[
              provider.disabled ? "disabled" : "enabled",
              // A provider with sign-in off only verifies: it backs linking and
              // invitations, never a sign-in button.
              ...(provider.allowSignIn ? [] : (["sign-in-off"] as const)),
              ...(provider.ready ? [] : (["not-ready"] as const)),
            ]}
            identifier={{
              value: provider.slug,
              label: t({
                id: "admin.federation.copy-slug",
                message: "Copy the slug",
              }),
            }}
            facts={
              protocol === undefined
                ? []
                : [
                    <span key="protocol">
                      {i18n._(providerProtocolLabel(protocol))}
                    </span>,
                  ]
            }
          />
          {!provider.ready && (
            <p
              data-slot="provider-not-ready"
              className="flex items-center gap-2 text-sm text-warning-foreground"
            >
              <TriangleAlertIcon
                aria-hidden="true"
                className="size-4 shrink-0"
              />
              {notReadyReason(provider)}
            </p>
          )}
        </div>
      }
    >
      <ProviderGeneralSection provider={provider} />

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

      {provider.protocol === "steam" && (
        <ProviderCredentialsSection provider={provider} />
      )}

      {provider.supportsOperator && (
        <Section
          id="operator"
          icon={<UserRoundCogIcon />}
          title={
            <Trans id="admin.federation.operator.title">Operator session</Trans>
          }
          description={
            <Trans id="admin.federation.operator.description">
              VRChat has no client credentials, so this instance signs in as an
              account of its own to read profiles.
            </Trans>
          }
        >
          <ProviderOperatorSection provider={provider} />
        </Section>
      )}

      <ProviderAppearanceSection provider={provider} />
      <ProviderDangerSection provider={provider} />
    </AppDetailLayout>
  );
}
