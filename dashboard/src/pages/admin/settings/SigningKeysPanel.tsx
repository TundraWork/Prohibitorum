import { plural } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { BracesIcon, KeyRoundIcon, PlayIcon } from "lucide-react";
import { useState } from "react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import {
  activateSigningKeyMutationOptions,
  generateSigningKeyMutationOptions,
  retireSigningKeyMutationOptions,
} from "@/api/mutations";
import { signingKeysListOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { CopyableValue } from "@/components/custom/CopyableValue";
import {
  ItemList,
  ItemListEmpty,
  ItemListRow,
} from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { Section, SectionFailure } from "@/components/custom/Section";
import { Badge } from "@/components/ui/badge";
import { PublicKeyDialog } from "@/pages/admin/settings/PublicKeyDialog";

type SigningKey = components["schemas"]["SigningKeyView"];

/**
 * The section's heading, drawn by the panel and by the boundary that waits for
 * the page, so the two cannot word it differently.
 */
export const signingKeysSection = {
  id: "signing-keys",
  icon: <KeyRoundIcon />,
  title: <Trans id="settings.keys.section">Signing keys</Trans>,
  description: (
    <Trans id="settings.keys.description">
      A new key is published as soon as it is generated and starts signing when
      you activate it. The key it replaces keeps verifying through a grace
      period, then is retired.
    </Trans>
  ),
};

type Pending = { action: "activate" | "retire"; key: SigningKey };

/** Where a key stands in its life; the server calls "retiring" decommissioning. */
const stages = ["active", "pending", "decommissioning", "retired"] as const;
const stageOf = (key: SigningKey) =>
  stages.indexOf(key.status as (typeof stages)[number]);

/**
 * The keys the instance signs tokens and SAML assertions with, read as a
 * lifecycle: the key that signs, the ones waiting to, the ones still verifying
 * what they signed, and the retired ones kept out of sight.
 *
 * A new key starts out pending: published for verification but not signing.
 * Activating one makes it the signer and starts retiring the previous one,
 * which stays published through the grace period so what it signed still
 * verifies. Only a pending key offers either action: the server answers retire
 * on a key that is already retiring by starting its grace period over, which
 * postpones the retirement rather than hastening it.
 */
export function SigningKeysPanel() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const list = useCursorList<SigningKey>(quietQuery(signingKeysListOptions()));
  const generate = useMutation(generateSigningKeyMutationOptions(queryClient));
  const activate = useMutation(activateSigningKeyMutationOptions(queryClient));
  const retire = useMutation(retireSigningKeyMutationOptions(queryClient));
  const [pending, setPending] = useState<Pending | null>(null);
  const [viewing, setViewing] = useState<SigningKey | null>(null);
  const [showRetired, setShowRetired] = useState(false);

  const current = list.items
    .filter((key) => key.status !== "retired")
    .sort((a, b) => stageOf(a) - stageOf(b));
  const retired = list.items.filter((key) => key.status === "retired");
  const visible = showRetired ? [...current, ...retired] : current;
  const failed = list.error !== null && list.error !== undefined;

  const confirm = () => {
    if (!pending) return;
    const mutation = pending.action === "activate" ? activate : retire;
    // Settled either way: a refusal has already refreshed the list and said
    // why in a toast, so there is nothing left for the dialog to offer.
    mutation.mutate(pending.key.kid, { onSettled: () => setPending(null) });
  };
  const dismiss = (open: boolean, mutation: { isPending: boolean }) => {
    if (!open && !mutation.isPending) setPending(null);
  };

  return (
    <>
      <Section
        {...signingKeysSection}
        action={
          <Button
            pending={generate.isPending}
            onClick={() => generate.mutate()}
          >
            <Trans id="settings.keys.generate">Generate key</Trans>
          </Button>
        }
      >
        {failed && list.items.length === 0 ? (
          <SectionFailure
            error={list.error}
            onRetry={list.refetch}
            title={
              <Trans id="settings.keys.error">
                The keys could not be loaded.
              </Trans>
            }
          />
        ) : (
          <ItemList
            label={t({ id: "settings.keys.table", message: "Signing keys" })}
            loading={list.loading}
            empty={
              <ItemListEmpty>
                {retired.length > 0 ? (
                  <Trans id="settings.keys.none_current">No current keys</Trans>
                ) : (
                  <Trans id="settings.keys.empty">No signing keys</Trans>
                )}
              </ItemListEmpty>
            }
            footer={
              (retired.length > 0 || list.hasMore) && (
                <div className="flex flex-wrap justify-center gap-2">
                  {retired.length > 0 && (
                    <Button
                      variant="ghost"
                      size="sm"
                      aria-expanded={showRetired}
                      onClick={() => setShowRetired((shown) => !shown)}
                    >
                      {showRetired ? (
                        <Trans id="settings.keys.retired.hide">
                          Hide retired keys
                        </Trans>
                      ) : (
                        t({
                          id: "settings.keys.retired.show",
                          message: plural(retired.length, {
                            one: "Show # retired key",
                            other: "Show # retired keys",
                          }),
                        })
                      )}
                    </Button>
                  )}
                  {list.hasMore && (
                    <Button
                      variant="ghost"
                      size="sm"
                      pending={list.loadingMore}
                      onClick={list.loadMore}
                    >
                      <Trans id="settings.keys.more">Load more</Trans>
                    </Button>
                  )}
                </div>
              )
            }
          >
            {visible.map((key) => (
              <KeyRow
                key={key.kid}
                signingKey={key}
                onView={() => setViewing(key)}
                onActivate={() => setPending({ action: "activate", key })}
                onRetire={() => setPending({ action: "retire", key })}
              />
            ))}
          </ItemList>
        )}
      </Section>

      <ConfirmDialog
        isOpen={pending?.action === "activate"}
        onOpenChange={(open) => dismiss(open, activate)}
        status="warning"
        title={
          <Trans id="settings.keys.activate.title">Sign with this key?</Trans>
        }
        body={
          <p>
            <Trans id="settings.keys.activate.body">
              New tokens are signed with this key. The current key keeps
              verifying what it signed until it is retired.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="settings.keys.activate.action">Activate</Trans>
        }
        isPending={activate.isPending}
        onConfirm={confirm}
      />
      <ConfirmDialog
        isOpen={pending?.action === "retire"}
        onOpenChange={(open) => dismiss(open, retire)}
        status="danger"
        title={<Trans id="settings.keys.retire.title">Retire this key?</Trans>}
        body={
          <p>
            <Trans id="settings.keys.retire.body">
              After the grace period it is removed from the JWKS and the SAML
              metadata.
            </Trans>
          </p>
        }
        confirmLabel={<Trans id="settings.keys.retire.action">Retire</Trans>}
        isPending={retire.isPending}
        onConfirm={confirm}
      />
      <PublicKeyDialog signingKey={viewing} onClose={() => setViewing(null)} />
    </>
  );
}

/** One key: its id, its algorithm, where it stands and what can be done next. */
function KeyRow({
  signingKey,
  onView,
  onActivate,
  onRetire,
}: {
  signingKey: SigningKey;
  onView: () => void;
  onActivate: () => void;
  onRetire: () => void;
}) {
  const { t, i18n } = useLingui();
  const { kid, status, activatedAt, retireAfter } = signingKey;
  const until =
    retireAfter === undefined
      ? undefined
      : new Intl.DateTimeFormat(i18n.locale, { dateStyle: "medium" }).format(
          new Date(retireAfter),
        );

  return (
    <ItemListRow
      icon={<KeyRoundIcon className="size-4" />}
      title={
        <CopyableValue
          value={kid}
          wrap
          label={t({ id: "settings.keys.kid.copy", message: "Copy key ID" })}
        />
      }
      badge={<KeyStatus status={status} />}
      details={[
        <span key="algorithm" className="font-mono">
          {signingKey.algorithm}
        </span>,
        <KeyStanding
          key="standing"
          status={status}
          activatedAt={activatedAt}
          retireAfter={retireAfter}
          until={until}
        />,
      ]}
      stackActions
      actions={
        <>
          <Button
            aria-label={t({
              id: "settings.keys.view",
              message: "View public key",
            })}
            onClick={onView}
            variant="ghost"
            size="icon-sm"
          >
            <BracesIcon aria-hidden="true" />
          </Button>
          {status === "pending" && (
            <>
              <Button onClick={onActivate} variant="outline" size="sm">
                <PlayIcon aria-hidden="true" />
                <Trans id="settings.keys.activate.action">Activate</Trans>
              </Button>
              <Button
                onClick={onRetire}
                variant="destructive-outline"
                size="sm"
              >
                <Trans id="settings.keys.retire.action">Retire</Trans>
              </Button>
            </>
          )}
        </>
      }
    />
  );
}

/** What the key is doing now, and for a pending one what comes next. */
function KeyStanding({
  status,
  activatedAt,
  retireAfter,
  until,
}: {
  status: string;
  activatedAt: string | undefined;
  retireAfter: string | undefined;
  until: string | undefined;
}) {
  switch (status) {
    case "active":
      return activatedAt === undefined ? (
        <Trans id="settings.keys.standing.signing">Signing</Trans>
      ) : (
        <Trans id="settings.keys.standing.active">
          Signing since <RelativeTime value={activatedAt} />
        </Trans>
      );
    case "pending":
      return (
        <Trans id="settings.keys.standing.pending">
          Published, not yet signing
        </Trans>
      );
    case "decommissioning":
      return until === undefined ? (
        <Trans id="settings.keys.standing.retiring_open">
          Still verifying what it signed
        </Trans>
      ) : (
        <Trans id="settings.keys.standing.retiring">
          Still verifying until {until}
        </Trans>
      );
    case "retired":
      return retireAfter === undefined ? (
        <Trans id="settings.keys.standing.retired_open">Retired</Trans>
      ) : (
        <Trans id="settings.keys.standing.retired">
          Retired <RelativeTime value={retireAfter} />
        </Trans>
      );
    default:
      return status;
  }
}

function KeyStatus({ status }: { status: string }) {
  switch (status) {
    case "active":
      return (
        <Badge variant="success">
          <Trans id="settings.keys.status.active">Signing</Trans>
        </Badge>
      );
    case "pending":
      return (
        <Badge variant="info">
          <Trans id="settings.keys.status.pending">Pending</Trans>
        </Badge>
      );
    case "decommissioning":
      return (
        <Badge variant="warning">
          <Trans id="settings.keys.status.decommissioning">Retiring</Trans>
        </Badge>
      );
    case "retired":
      return (
        <Badge variant="outline">
          <Trans id="settings.keys.status.retired">Retired</Trans>
        </Badge>
      );
    default:
      return <Badge variant="outline">{status}</Badge>;
  }
}
