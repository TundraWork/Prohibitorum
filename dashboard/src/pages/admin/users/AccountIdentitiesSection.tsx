import { Trans, useLingui } from "@lingui/react/macro";
import { useQuery } from "@tanstack/react-query";
import { Link2 } from "lucide-react";
import { Fragment, type ReactNode } from "react";
import type { components } from "@/api/generated/schema";
import { accountIdentitiesQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { ItemListRow } from "@/components/custom/ItemList";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { Badge } from "@/components/ui/badge";
import { providerProtocolLabel } from "@/pages/admin/identity-providers/provider-options";
import { AccessSection } from "@/pages/admin/users/AccessSection";

type Identity = components["schemas"]["AccountIdentityView"];

/** The protocol in words; one this console has no word for is shown as sent. */
function ProtocolBadge({ protocol }: { protocol: Identity["protocol"] }) {
  const { i18n } = useLingui();
  if (protocol === "oidc" || protocol === "steam" || protocol === "vrchat") {
    return (
      <Badge variant="outline">{i18n._(providerProtocolLabel(protocol))}</Badge>
    );
  }
  return (
    <Badge variant="outline">
      {protocol === "saml" ? <Trans id="apps.kind.saml">SAML</Trans> : protocol}
    </Badge>
  );
}

/**
 * A subject with a break point after each separator it carries, so a long
 * name identifier wraps between its parts rather than mid-segment. The break
 * points are empty elements, so a selection and a copy are the subject itself.
 */
function breakAtSeparators(subject: string): ReactNode {
  return subject.split(/(?<=[:/|!#@])/).map((part, index) => (
    // The parts are positional and never reordered.
    // biome-ignore lint/suspicious/noArrayIndexKey: see above
    <Fragment key={index}>
      {index > 0 && <wbr />}
      {part}
    </Fragment>
  ));
}

/**
 * The other providers an account signs in with. Read-only: unlinking one is
 * the account holder's choice, because it changes how they get in.
 */
export function AccountIdentitiesSection({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const identities = useQuery(
    quietQuery(accountIdentitiesQueryOptions(accountId)),
  );

  return (
    <AccessSection
      id="identities"
      icon={<Link2 />}
      title={
        <Trans id="admin.user.identities.title">Connected identities</Trans>
      }
      description={
        <Trans id="admin.user.identities.description">
          Accounts at other providers they can sign in with. Administrators
          cannot unlink them; only the account holder can.
        </Trans>
      }
      label={t({
        id: "admin.user.identities.table",
        message: "Connected identities",
      })}
      query={identities}
      writeError={null}
      empty={
        <TableEmptyState
          icon={<Link2 size={18} strokeWidth={1.75} aria-hidden="true" />}
          title={
            <Trans id="admin.user.identities.empty">
              No connected identities
            </Trans>
          }
        />
      }
    >
      {(identities.data ?? []).map((identity) => (
        <ItemListRow
          key={identity.id}
          icon={<EntityAvatar size="md" name={identity.providerDisplayName} />}
          title={identity.providerDisplayName}
          badges={<ProtocolBadge protocol={identity.protocol} />}
          details={[
            <span key="subject" className="font-mono">
              {breakAtSeparators(identity.subject)}
            </span>,
          ]}
        />
      ))}
    </AccessSection>
  );
}
