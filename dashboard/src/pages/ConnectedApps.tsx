import { Alert, AlertDialog, Avatar, Chip } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { AppWindow, Trash2 } from "lucide-react";
import { useState } from "react";
import { revokeConsentMutationOptions } from "@/api/mutations";
import { consentQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

type ConsentedApp = {
  clientId: string;
  kind: string;
  name: string;
  iconUrl?: string;
  grantedAt: string;
  scopes: string[] | null;
};

/**
 * Applications the account has granted access to. This is revocation only: the
 * launchpad that used to live here needs its own consent rules and is not part
 * of this page.
 */
export function ConnectedApps() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const consent = useQuery(consentQueryOptions());
  const [target, setTarget] = useState<ConsentedApp | null>(null);
  const [confirming, setConfirming] = useState(false);

  const revoke = useMutation(revokeConsentMutationOptions(queryClient));

  return (
    <>
      {consent.isError && (
        <Alert status="danger" role="alert">
          <Alert.Indicator />
          <Alert.Content>
            <Alert.Title>
              {t({
                id: "apps.load_failed",
                message: "Could not load your connected applications.",
              })}
            </Alert.Title>
          </Alert.Content>
        </Alert>
      )}

      <ItemList
        label={t({
          id: "apps.table.label",
          message: "Connected applications",
        })}
        loading={consent.isPending}
        empty={
          <TableEmptyState
            icon={<AppWindow size={18} strokeWidth={1.75} aria-hidden="true" />}
            title={<Trans id="apps.empty">No approved applications yet</Trans>}
          />
        }
      >
        {(consent.data ?? []).map((app) => (
          <ItemListRow
            key={`${app.kind}:${app.clientId}`}
            icon={
              <Avatar className="size-9 rounded-[0.375rem]">
                {app.iconUrl && <Avatar.Image src={app.iconUrl} alt="" />}
                <Avatar.Fallback className="rounded-[0.375rem]">
                  <AppWindow size={18} aria-hidden="true" />
                </Avatar.Fallback>
              </Avatar>
            }
            title={app.name}
            badges={
              <Chip size="sm" variant="soft">
                {app.kind === "saml" ? (
                  <Trans id="apps.kind.saml">SAML</Trans>
                ) : (
                  <Trans id="apps.kind.oidc">OpenID Connect</Trans>
                )}
              </Chip>
            }
            details={[
              <Trans key="granted" id="apps.detail.granted">
                Authorized <RelativeTime value={app.grantedAt} />
              </Trans>,
              app.scopes && app.scopes.length > 0 ? (
                app.scopes.join(", ")
              ) : (
                <Trans key="scopes" id="apps.scopes.none">
                  Basic profile
                </Trans>
              ),
            ]}
            actions={
              <Button
                isIconOnly
                size="sm"
                variant="danger-soft"
                aria-label={t({ id: "apps.remove", message: "Remove access" })}
                onPress={() => {
                  setTarget(app);
                  setConfirming(true);
                }}
              >
                <Trash2 size={16} aria-hidden="true" />
              </Button>
            }
          />
        ))}
      </ItemList>

      <AlertDialog isOpen={confirming} onOpenChange={setConfirming}>
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>
                  <Trans id="apps.confirm.title">Remove access?</Trans>
                </AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>
                <p>
                  <Trans id="apps.confirm.body">
                    This application will no longer read your profile. It will
                    need to ask for permission again.
                  </Trans>
                </p>
              </AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  onPress={() => setConfirming(false)}
                  isDisabled={revoke.isPending}
                >
                  <Trans id="apps.confirm.cancel">Keep access</Trans>
                </Button>
                <Button
                  variant="danger"
                  isPending={revoke.isPending}
                  onPress={() => {
                    if (!target) return;
                    // `kind` is echoed exactly as listed: the server uses it to
                    // tell an OIDC client id from a SAML entity id.
                    revoke.mutate(
                      { clientId: target.clientId, kind: target.kind },
                      {
                        onSuccess: () => {
                          setConfirming(false);
                          setTarget(null);
                        },
                        // The global toast reports what went wrong.
                        onError: () => setConfirming(false),
                      },
                    );
                  }}
                >
                  <Trans id="apps.confirm.remove">Remove access</Trans>
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </>
  );
}
