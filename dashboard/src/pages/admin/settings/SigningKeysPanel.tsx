import { Alert, Chip, Modal, Tooltip } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import {
  Braces,
  Check,
  ClipboardCopy,
  KeyRound,
  Play,
  Trash2,
} from "lucide-react";
import { useState } from "react";
import { useCursorList } from "@/api/cursor-list";
import type { components } from "@/api/generated/schema";
import {
  activateSigningKeyMutationOptions,
  generateSigningKeyMutationOptions,
  retireSigningKeyMutationOptions,
} from "@/api/mutations";
import { signingKeysListOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { ScrollArea } from "@/components/custom/ScrollArea";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

type SigningKey = components["schemas"]["SigningKeyView"];
type Locale = ReturnType<typeof useLingui>["i18n"];

const actionLabels = {
  view: msg({ id: "settings.keys.view", message: "View public key" }),
  activate: msg({ id: "settings.keys.activate.action", message: "Activate" }),
  retire: msg({ id: "settings.keys.retire.action", message: "Retire" }),
};

type Pending =
  | { action: "activate"; key: SigningKey }
  | { action: "retire"; key: SigningKey };

/**
 * The keys the instance signs tokens and SAML assertions with.
 *
 * A new key starts out pending: published for verification but not signing.
 * Activating one makes it the signer and starts retiring the previous one,
 * which stays published through the grace period so what it signed still
 * verifies. Only a pending key offers either action: the server answers retire
 * on a key that is already retiring by starting its grace period over, which
 * postpones the retirement rather than hastening it.
 *
 * The list is the whole tab, so it sits on the page background under its one
 * action, like the directory lists.
 */
export function SigningKeysPanel() {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const list = useCursorList<SigningKey>(signingKeysListOptions());
  const generate = useMutation(generateSigningKeyMutationOptions(queryClient));
  const activate = useMutation(activateSigningKeyMutationOptions(queryClient));
  const retire = useMutation(retireSigningKeyMutationOptions(queryClient));
  const [pending, setPending] = useState<Pending | null>(null);
  const [viewing, setViewing] = useState<SigningKey | null>(null);

  const confirm = () => {
    if (!pending) return;
    const mutation = pending.action === "activate" ? activate : retire;
    // Settled either way: a refusal has already refreshed the list and said
    // why in a toast, so there is nothing left for the dialog to offer.
    mutation.mutate(pending.key.kid, { onSettled: () => setPending(null) });
  };

  return (
    <>
      <Section
        title={<Trans id="settings.keys.section">Signing keys</Trans>}
        action={
          <Button
            isPending={generate.isPending}
            onPress={() => generate.mutate()}
          >
            <Trans id="settings.keys.generate">Generate key</Trans>
          </Button>
        }
      >
        {list.error !== null && list.error !== undefined && (
          <Alert status="danger" role="alert">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Title>
                <Trans id="settings.keys.error">
                  The keys could not be loaded. Try again.
                </Trans>
              </Alert.Title>
            </Alert.Content>
          </Alert>
        )}

        <DataTable
          label={t({ id: "settings.keys.table", message: "Signing keys" })}
          columns={keyColumns(i18n, {
            view: setViewing,
            activate: (key) => setPending({ action: "activate", key }),
            retire: (key) => setPending({ action: "retire", key }),
          })}
          rows={list.items}
          rowId={(key) => key.kid}
          loading={list.loading}
          hasMore={list.hasMore}
          loadingMore={list.loadingMore}
          onLoadMore={list.loadMore}
          empty={
            <TableEmptyState
              icon={
                <KeyRound size={18} strokeWidth={1.75} aria-hidden="true" />
              }
              title={<Trans id="settings.keys.empty">No signing keys</Trans>}
            />
          }
        />
      </Section>

      <ConfirmDialog
        isOpen={pending?.action === "activate"}
        onOpenChange={(open) => !open && setPending(null)}
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
        confirmLabel={t(actionLabels.activate)}
        isPending={activate.isPending}
        onConfirm={confirm}
      />
      <ConfirmDialog
        isOpen={pending?.action === "retire"}
        onOpenChange={(open) => !open && setPending(null)}
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
        confirmLabel={t(actionLabels.retire)}
        isPending={retire.isPending}
        onConfirm={confirm}
      />
      <PublicKeyDialog signingKey={viewing} onClose={() => setViewing(null)} />
    </>
  );
}

function keyColumns(
  i18n: Locale,
  actions: {
    view: (key: SigningKey) => void;
    activate: (key: SigningKey) => void;
    retire: (key: SigningKey) => void;
  },
): TableColumn<SigningKey>[] {
  return [
    {
      id: "kid",
      header: <Trans id="settings.keys.column.kid">Key ID</Trans>,
      cell: (key) => <KeyId kid={key.kid} />,
    },
    {
      id: "algorithm",
      header: <Trans id="settings.keys.column.algorithm">Algorithm</Trans>,
      cell: (key) => key.algorithm,
    },
    {
      id: "status",
      header: <Trans id="settings.keys.column.status">Status</Trans>,
      cell: (key) => <KeyStatus status={key.status} />,
    },
    {
      align: "end",
      id: "activatedAt",
      header: <Trans id="settings.keys.column.activated">Activated</Trans>,
      cell: (key) =>
        key.activatedAt === undefined ? (
          "—"
        ) : (
          <RelativeTime value={key.activatedAt} />
        ),
    },
    {
      align: "end",
      id: "retireAfter",
      header: <Trans id="settings.keys.column.retires">Retires</Trans>,
      cell: (key) =>
        key.retireAfter === undefined ? (
          "—"
        ) : (
          <RelativeTime value={key.retireAfter} />
        ),
    },
    {
      id: "actions",
      header: <Trans id="admin.column.actions">Actions</Trans>,
      cell: (key) => (
        <>
          <Button
            isIconOnly
            size="sm"
            variant="tertiary"
            aria-label={i18n._(actionLabels.view)}
            onPress={() => actions.view(key)}
          >
            <Braces size={16} aria-hidden="true" />
          </Button>
          {key.status === "pending" && (
            <>
              <Button
                isIconOnly
                size="sm"
                variant="tertiary"
                aria-label={i18n._(actionLabels.activate)}
                onPress={() => actions.activate(key)}
              >
                <Play size={16} aria-hidden="true" />
              </Button>
              <Button
                isIconOnly
                size="sm"
                variant="danger-soft"
                aria-label={i18n._(actionLabels.retire)}
                onPress={() => actions.retire(key)}
              >
                <Trash2 size={16} aria-hidden="true" />
              </Button>
            </>
          )}
        </>
      ),
      pinned: true,
    },
  ];
}

/**
 * A key id is a 43-character thumbprint: the row shows its ends, the tooltip
 * the whole of it, and the button beside it copies it — which is also how a
 * keyboard reaches the whole id.
 */
function KeyId({ kid }: { kid: string }) {
  const { t } = useLingui();
  const [copied, setCopied] = useState(false);
  const short = kid.length > 12 ? `${kid.slice(0, 6)}…${kid.slice(-4)}` : kid;
  return (
    <span className="flex items-center gap-1">
      <Tooltip delay={0}>
        <Tooltip.Trigger<"span"> render={(props) => <span {...props} />}>
          {/* A screen reader reads the whole id; the ends are for the eye. */}
          <span className="font-mono" aria-hidden="true">
            {short}
          </span>
          <span className="sr-only">{kid}</span>
        </Tooltip.Trigger>
        <Tooltip.Content className="font-mono">{kid}</Tooltip.Content>
      </Tooltip>
      <Button
        isIconOnly
        size="sm"
        variant="ghost"
        aria-label={
          copied
            ? t({ id: "settings.keys.kid.copied", message: "Copied" })
            : t({ id: "settings.keys.kid.copy", message: "Copy key ID" })
        }
        onPress={() => {
          void navigator.clipboard.writeText(kid).then(
            () => setCopied(true),
            () => setCopied(false),
          );
        }}
      >
        {copied ? (
          <Check size={14} aria-hidden="true" />
        ) : (
          <ClipboardCopy size={14} aria-hidden="true" />
        )}
      </Button>
    </span>
  );
}

function KeyStatus({ status }: { status: string }) {
  switch (status) {
    case "active":
      return (
        <Chip color="success" size="sm" variant="soft">
          <Trans id="settings.keys.status.active">Signing</Trans>
        </Chip>
      );
    case "pending":
      return (
        <Chip color="accent" size="sm" variant="soft">
          <Trans id="settings.keys.status.pending">Pending</Trans>
        </Chip>
      );
    case "decommissioning":
      return (
        <Chip color="warning" size="sm" variant="soft">
          <Trans id="settings.keys.status.decommissioning">Retiring</Trans>
        </Chip>
      );
    case "retired":
      return (
        <Chip color="default" size="sm" variant="soft">
          <Trans id="settings.keys.status.retired">Retired</Trans>
        </Chip>
      );
    default:
      return <span>{status}</span>;
  }
}

/**
 * The public half of a key as the JWKS publishes it. Nothing here is secret,
 * so it is a plain dialog with a copy action rather than a `SecretReveal`.
 */
function PublicKeyDialog({
  signingKey,
  onClose,
}: {
  signingKey: SigningKey | null;
  onClose: () => void;
}) {
  const [copy, setCopy] = useState<"idle" | "copied" | "failed">("idle");
  const jwk =
    signingKey === null ? "" : JSON.stringify(signingKey.publicJwk, null, 2);

  return (
    <Modal
      isOpen={signingKey !== null}
      onOpenChange={(open) => {
        if (open) return;
        setCopy("idle");
        onClose();
      }}
    >
      <Modal.Backdrop>
        <Modal.Container placement="center" size="lg" scroll="inside">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>
                <Trans id="settings.keys.jwk.title">Public key</Trans>
              </Modal.Heading>
            </Modal.Header>
            <Modal.Body className="flex flex-col gap-3">
              {/* The background and radius stay on the `ScrollArea` host rather
                  than on the `<pre>`: the `<pre>` is only as wide as the
                  viewport, so painting the surface there would leave the
                  scrolled-in half of a long JWK on the modal's own backdrop. */}
              <ScrollArea className="rounded-[0.375rem] bg-surface-secondary p-3 font-mono text-xs">
                <pre>{jwk}</pre>
              </ScrollArea>
              {copy === "failed" && (
                <SurfaceAlert status="danger" role="alert">
                  <SurfaceAlert.Indicator />
                  <SurfaceAlert.Content>
                    <SurfaceAlert.Title>
                      <Trans id="settings.keys.jwk.copy.failed">
                        Could not copy. Your browser blocked the clipboard;
                        allow it and try again.
                      </Trans>
                    </SurfaceAlert.Title>
                  </SurfaceAlert.Content>
                </SurfaceAlert>
              )}
            </Modal.Body>
            <Modal.Footer>
              <Button
                variant="tertiary"
                onPress={() => {
                  void navigator.clipboard.writeText(jwk).then(
                    () => setCopy("copied"),
                    () => setCopy("failed"),
                  );
                }}
              >
                {copy === "copied" ? (
                  <>
                    <Check size={16} aria-hidden="true" />
                    <Trans id="settings.keys.jwk.copied">Copied</Trans>
                  </>
                ) : (
                  <>
                    <ClipboardCopy size={16} aria-hidden="true" />
                    <Trans id="settings.keys.jwk.copy">Copy</Trans>
                  </>
                )}
              </Button>
              <Button
                onPress={() => {
                  setCopy("idle");
                  onClose();
                }}
              >
                <Trans id="settings.keys.jwk.close">Close</Trans>
              </Button>
            </Modal.Footer>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
