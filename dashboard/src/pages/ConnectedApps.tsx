import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { AppWindow, Trash2 } from "lucide-react";
import { useState } from "react";
import type { components } from "@/api/generated/schema";
import { revokeConsentMutationOptions } from "@/api/mutations";
import {
  consentQueryOptions,
  federationProvidersQueryOptions,
} from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { AsyncSection } from "@/components/custom/Section";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { Badge } from "@/components/ui/badge";
import { describeScope } from "@/lib/scopes";

type ConsentedApp = components["schemas"]["ConsentedApp"];

/**
 * Applications the account has granted access to. This is revocation only: the
 * launchpad that used to live here needs its own consent rules and is not part
 * of this page.
 */
export function ConnectedApps() {
  return (
    <PageBody>
      <PageHeader
        title={<Trans id="apps.page.title">Connected applications</Trans>}
        description={
          <Trans id="apps.page.description">
            Applications you allowed to sign you in. Removing one makes it ask
            again next time.
          </Trans>
        }
      />
      <AsyncSection resetKey="connected-apps">
        <ConnectedAppList />
      </AsyncSection>
    </PageBody>
  );
}

function ConnectedAppList() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const { data } = useSuspenseQuery(consentQueryOptions());
  // Names the provider behind an `identity:<slug>` scope. A cosmetic lookup:
  // until it arrives, or if it fails, the slug stands in and nothing is reported.
  const providers = useQuery(quietQuery(federationProvidersQueryOptions()));
  const providerNames = Object.fromEntries(
    (providers.data ?? []).map((p) => [p.slug, p.displayName]),
  );
  const [target, setTarget] = useState<ConsentedApp | null>(null);
  const { error, clear, report } = useInlineFailure();

  // The failure is drawn on this page, so the toast stays silent.
  const revoke = useMutation(
    withInlineReport(revokeConsentMutationOptions(queryClient), true),
  );

  return (
    <div className="flex flex-col gap-4">
      <InlineFailure error={error} />

      <ItemList
        label={t({
          id: "apps.table.label",
          message: "Connected applications",
        })}
        empty={
          <TableEmptyState
            icon={<AppWindow size={18} strokeWidth={1.75} aria-hidden="true" />}
            title={<Trans id="apps.empty">No approved applications yet</Trans>}
          />
        }
      >
        {data.map((app) => (
          <ItemListRow
            key={`${app.kind}:${app.clientId}`}
            icon={
              <EntityAvatar size="md" name={app.name} iconUrl={app.iconUrl} />
            }
            title={app.name}
            badge={
              <Badge variant="outline">
                {app.kind === "saml" ? (
                  <Trans id="apps.kind.saml">SAML</Trans>
                ) : (
                  <Trans id="apps.kind.oidc">OpenID Connect</Trans>
                )}
              </Badge>
            }
            meta={
              <Trans id="apps.meta.allowed">
                Allowed <RelativeTime value={app.grantedAt} />
              </Trans>
            }
            details={[
              <GrantedScopes
                key="scopes"
                scopes={app.scopes}
                providers={providerNames}
              />,
            ]}
            actions={
              <Button
                aria-label={t({ id: "apps.remove", message: "Remove access" })}
                onClick={() => setTarget(app)}
                variant="destructive-outline"
                size="icon-sm"
              >
                <Trash2 aria-hidden="true" />
              </Button>
            }
          />
        ))}
      </ItemList>

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
        status="danger"
        title={<Trans id="apps.confirm.title">Remove access?</Trans>}
        body={
          <p>
            <Trans id="apps.confirm.body">
              This application will no longer read your profile. It will need to
              ask for permission again.
            </Trans>
          </p>
        }
        cancelLabel={<Trans id="apps.confirm.cancel">Keep access</Trans>}
        confirmLabel={<Trans id="apps.confirm.remove">Remove access</Trans>}
        isPending={revoke.isPending}
        onConfirm={() => {
          if (!target) return;
          clear();
          // `kind` is echoed exactly as listed: the server uses it to tell an
          // OIDC client id from a SAML entity id.
          revoke.mutate(
            { clientId: target.clientId, kind: target.kind },
            {
              onSuccess: () => setTarget(null),
              onError: (failure) => {
                report(failure);
                setTarget(null);
              },
            },
          );
        }}
      />
    </div>
  );
}

/**
 * What the application may read, named as the consent page named it. A scope
 * the application defined has no wording of ours and stays as written.
 */
function GrantedScopes({
  scopes,
  providers,
}: {
  scopes: string[] | null;
  providers: Readonly<Record<string, string>>;
}) {
  const { i18n } = useLingui();
  if (scopes === null || scopes.length === 0) {
    return <Trans id="apps.scopes.none">Basic profile</Trans>;
  }
  // Joined into a sentence, so every wording after the first starts in lower
  // case; a scope the application defined is shown as written.
  const names = scopes.map((scope, index) => {
    const { name, values } = describeScope(scope, providers);
    if (!name) return scope;
    const text = i18n._({ ...name, values });
    return index === 0
      ? text
      : text.charAt(0).toLocaleLowerCase(i18n.locale) + text.slice(1);
  });
  return new Intl.ListFormat(i18n.locale, {
    style: "long",
    type: "conjunction",
  }).format(names);
}
