import { Trans, useLingui } from "@lingui/react/macro";
import { useSuspenseQuery } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { AppWindowIcon } from "lucide-react";
import type { ReactNode } from "react";
import { groupApplicationsQueryOptions } from "@/api/queries";
import type { GroupApplicationView } from "@/api/raw-admin-paths";
import { quietQuery } from "@/app/query-client";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { AsyncSection, Section } from "@/components/custom/Section";
import { Badge } from "@/components/ui/badge";

/** The kind of an application, in words, for the badge on its row. */
function kindLabel(kind: string): ReactNode {
  if (kind === "saml") return <Trans id="apps.kind.saml">SAML</Trans>;
  if (kind === "forward_auth") {
    return (
      <Trans id="admin.group.used-by.kind.forward-auth">Forward-auth</Trans>
    );
  }
  return <Trans id="apps.kind.oidc">OpenID Connect</Trans>;
}

/** A link to the application's own page, whichever kind it is. */
function applicationLink(application: GroupApplicationView) {
  return ({
    className,
    children,
  }: {
    className: string;
    children: ReactNode;
  }) => {
    if (application.kind === "saml") {
      return (
        <Link
          to="/admin/saml-applications/$id"
          params={{ id: application.appId }}
          className={className}
        >
          {children}
        </Link>
      );
    }
    if (application.kind === "forward_auth") {
      return (
        <Link
          to="/admin/forward-auth-apps/$clientId"
          params={{ clientId: application.appId }}
          className={className}
        >
          {children}
        </Link>
      );
    }
    return (
      <Link
        to="/admin/oidc-applications/$clientId"
        params={{ clientId: application.appId }}
        className={className}
      >
        {children}
      </Link>
    );
  };
}

const heading = {
  id: "used-by",
  title: <Trans id="admin.group.used-by.title">Used by</Trans>,
  icon: <AppWindowIcon />,
};

/**
 * The applications that select the group, each linking to its own page: what
 * changes if the group is edited or deleted.
 */
export function GroupUsedBySection({ groupId }: { groupId: number }) {
  return (
    <AsyncSection resetKey={`group-${groupId}-used-by`} {...heading}>
      <UsedByList groupId={groupId} />
    </AsyncSection>
  );
}

function UsedByList({ groupId }: { groupId: number }) {
  const { t } = useLingui();
  const { data } = useSuspenseQuery(
    quietQuery(groupApplicationsQueryOptions(groupId)),
  );
  const applications = data.items ?? [];

  return (
    <Section {...heading}>
      <ItemList
        label={t({ id: "admin.group.used-by.label", message: "Applications" })}
        empty={
          <p className="text-pretty px-4 py-6 text-center text-muted-foreground text-sm">
            <Trans id="admin.group.used-by.empty">
              No application uses this group.
            </Trans>
          </p>
        }
      >
        {applications.map((application) => (
          <ItemListRow
            key={`${application.kind}-${application.appId}`}
            icon={
              <EntityAvatar
                size="sm"
                iconUrl={application.iconUrl}
                name={application.displayName}
              />
            }
            title={application.displayName}
            badges={
              <Badge variant="outline">{kindLabel(application.kind)}</Badge>
            }
            link={applicationLink(application)}
          />
        ))}
      </ItemList>
    </Section>
  );
}
