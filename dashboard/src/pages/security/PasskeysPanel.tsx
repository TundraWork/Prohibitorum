import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import {
  Fingerprint,
  KeyRound,
  Laptop,
  Pencil,
  Plus,
  Smartphone,
  Trash2,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  addCredentialMutationOptions,
  deleteCredentialMutationOptions,
  renameCredentialMutationOptions,
} from "@/api/mutations";
import { credentialsQueryOptions } from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import {
  InlineFailure,
  useInlineFailure,
} from "@/components/custom/InlineFailure";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { RowMenu } from "@/components/custom/RowMenu";
import { AsyncSection, Section } from "@/components/custom/Section";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { getTopLayerContainer } from "@/components/custom/top-layer";
import { Badge } from "@/components/ui/badge";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldDescription, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { MenuItem } from "@/components/ui/menu";

type Credential = components["schemas"]["CredentialView"];

/**
 * What the transports say about where the key lives: a phone for `hybrid`, the
 * device itself for `internal`, and a key otherwise. The server cannot tell a
 * laptop from a desktop, so the icon only separates the three.
 */
function CredentialIcon({ transports }: { transports: string[] | null }) {
  if (transports?.includes("hybrid")) return <Smartphone />;
  if (transports?.includes("internal")) return <Laptop />;
  return <KeyRound />;
}

/**
 * Passkeys on the account. Adding one runs the full WebAuthn registration
 * ceremony, which the server refuses without a fresh sudo window, so the sudo
 * prompt appears first — the user should understand why the browser is about to
 * ask for a passkey before it does.
 *
 * The heading, icon and description are the boundary's too, so they stay put
 * while the list loads.
 */
export function PasskeysPanel() {
  return (
    <AsyncSection
      resetKey="passkeys"
      icon={<Fingerprint />}
      title={<PasskeysTitle />}
      description={<PasskeysDescription />}
    >
      <PasskeyList />
    </AsyncSection>
  );
}

function PasskeysTitle() {
  return <Trans id="security.section.passkeys">Passkeys</Trans>;
}

function PasskeysDescription() {
  return (
    <Trans id="security.passkeys.description">
      The way in. Your device keeps the key, so there's nothing to type or leak.
    </Trans>
  );
}

function PasskeyList() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const { data } = useSuspenseQuery(quietQuery(credentialsQueryOptions()));
  const { error, clear, report } = useInlineFailure();
  const [renaming, setRenaming] = useState<Credential | null>(null);
  const [deleting, setDeleting] = useState<Credential | null>(null);

  // Each failure is drawn in this section, so the toast stays silent.
  const add = useMutation(
    withInlineReport(addCredentialMutationOptions(queryClient), true),
  );
  const remove = useMutation(
    withInlineReport(deleteCredentialMutationOptions(queryClient), true),
  );
  // Held here, not in the dialog: dismissing the dialog mid-save must not
  // drop the failure, and only a mounted mutation runs its callbacks.
  const rename = useMutation(
    withInlineReport(renameCredentialMutationOptions(queryClient), true),
  );

  const rows = data;
  // The server rejects removing the last passkey; saying so in the row and
  // disabling the item shows the reason before the attempt.
  const onlyOne = rows.length === 1;

  return (
    <>
      <Section
        icon={<Fingerprint />}
        title={<PasskeysTitle />}
        description={<PasskeysDescription />}
        action={
          <Button
            size="sm"
            pending={add.isPending}
            onClick={() => {
              clear();
              add.mutate(undefined, { onError: report });
            }}
          >
            <Plus />
            <Trans id="security.passkeys.add">Add a passkey</Trans>
          </Button>
        }
      >
        <InlineFailure error={error} />
        <ItemList
          label={t({ id: "security.passkeys.table", message: "Passkeys" })}
          empty={
            <TableEmptyState
              icon={
                <Fingerprint size={18} strokeWidth={1.75} aria-hidden="true" />
              }
              title={
                <Trans id="security.passkeys.empty.title">
                  No passkeys yet
                </Trans>
              }
            />
          }
        >
          {rows.map((credential) => (
            <PasskeyRow
              key={credential.id}
              credential={credential}
              onlyOne={onlyOne}
              onRename={() => setRenaming(credential)}
              onRemove={() => setDeleting(credential)}
            />
          ))}
        </ItemList>
      </Section>

      <RenameDialog
        credential={renaming}
        pending={rename.isPending}
        onClose={() => setRenaming(null)}
        onSave={(nickname) => {
          if (!renaming) return;
          rename.mutate(
            { id: renaming.id, nickname },
            {
              onSuccess: () => setRenaming(null),
              onError: (failure) => {
                report(failure);
                setRenaming(null);
              },
            },
          );
        }}
      />

      <ConfirmDialog
        isOpen={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
        status="danger"
        title={
          <Trans id="security.passkeys.delete.title">
            Remove this passkey?
          </Trans>
        }
        body={
          <p>
            <Trans id="security.passkeys.delete.body">
              This passkey will stop working for sign-in right away. Any other
              way you have to sign in is unaffected.
            </Trans>
          </p>
        }
        cancelLabel={<Trans id="security.keep">Keep it</Trans>}
        confirmLabel={
          <Trans id="security.passkeys.delete.confirm">Remove</Trans>
        }
        isPending={remove.isPending}
        onConfirm={() => {
          if (!deleting) return;
          remove.mutate(deleting.id, {
            onSuccess: () => setDeleting(null),
            onError: (failure) => {
              // Backing out of the sudo prompt returns to this question.
              if (isCancellation(failure)) return;
              report(failure);
              setDeleting(null);
            },
          });
        }}
      />
    </>
  );
}

function PasskeyRow({
  credential,
  onlyOne,
  onRename,
  onRemove,
}: {
  credential: Credential;
  onlyOne: boolean;
  onRename: () => void;
  onRemove: () => void;
}) {
  const { t } = useLingui();
  const unnamed = t({
    id: "security.passkeys.unnamed",
    message: "Unnamed passkey",
  });
  const name = credential.nickname || unnamed;
  const lastUsed = credential.lastUsedAt;
  const details: ReactNode[] = [
    <Trans key="added" id="security.passkeys.detail.added">
      Added <RelativeTime value={credential.createdAt} />
    </Trans>,
  ];
  if (onlyOne) {
    details.push(
      <Trans key="only" id="security.passkeys.only_one">
        Your only passkey
      </Trans>,
    );
  }
  details.push(
    <span key="suffix" className="font-mono">
      …{credential.credentialIdSuffix}
    </span>,
  );

  return (
    <ItemListRow
      icon={<CredentialIcon transports={credential.transports} />}
      title={
        credential.nickname || (
          <span className="text-muted-foreground">{unnamed}</span>
        )
      }
      badge={
        credential.backupState ? (
          <Badge variant="success">
            <Trans id="security.passkeys.synced">Synced</Trans>
          </Badge>
        ) : (
          <Badge variant="outline">
            <Trans id="security.passkeys.device_bound">This device only</Trans>
          </Badge>
        )
      }
      meta={
        lastUsed === undefined ? (
          <Trans id="security.passkeys.never_used">Not used yet</Trans>
        ) : (
          <Trans id="security.passkeys.meta.used">
            Used <RelativeTime value={lastUsed} />
          </Trans>
        )
      }
      details={details}
      actions={
        <RowMenu
          label={t({
            id: "security.passkeys.more",
            message: `More actions for ${name}`,
          })}
        >
          <MenuItem onClick={onRename}>
            <Pencil />
            <Trans id="security.passkeys.rename">Rename</Trans>
          </MenuItem>
          <MenuItem
            variant="destructive"
            disabled={onlyOne}
            onClick={onRemove}
            className="data-disabled:data-[variant=destructive]:text-muted-foreground"
          >
            <Trash2 />
            <Trans id="security.passkeys.delete">Remove</Trans>
          </MenuItem>
        </RowMenu>
      }
    />
  );
}

/**
 * The name field for one passkey. It holds its own draft, remounted per
 * passkey, so the draft never outlives the dialog that asked for it.
 */
function RenameDialog({
  credential,
  pending,
  onClose,
  onSave,
}: {
  credential: Credential | null;
  pending: boolean;
  onClose: () => void;
  onSave: (nickname: string) => void;
}) {
  return (
    <Dialog
      open={credential !== null}
      onOpenChange={(open) => !open && onClose()}
    >
      <DialogPopup
        className="sm:max-w-md"
        portalProps={{ container: getTopLayerContainer() }}
      >
        {credential && (
          <RenameForm
            key={credential.id}
            credential={credential}
            pending={pending}
            onClose={onClose}
            onSave={onSave}
          />
        )}
      </DialogPopup>
    </Dialog>
  );
}

function RenameForm({
  credential,
  pending,
  onClose,
  onSave,
}: {
  credential: Credential;
  pending: boolean;
  onClose: () => void;
  onSave: (nickname: string) => void;
}) {
  const { t } = useLingui();
  const [nickname, setNickname] = useState(credential.nickname ?? "");

  return (
    <form
      className="flex min-h-0 flex-1 flex-col"
      aria-label={t({
        id: "security.passkeys.rename.title",
        message: "Rename this passkey",
      })}
      onSubmit={(event) => {
        event.preventDefault();
        if (!pending) onSave(nickname);
      }}
    >
      <DialogHeader>
        <DialogTitle>
          <Trans id="security.passkeys.rename.title">Rename this passkey</Trans>
        </DialogTitle>
      </DialogHeader>
      <DialogPanel>
        <Field>
          <FieldLabel>
            <Trans id="security.passkeys.rename.label">Name</Trans>
          </FieldLabel>
          <Input
            autoComplete="off"
            value={nickname}
            onValueChange={setNickname}
          />
          <FieldDescription>
            <Trans id="security.passkeys.rename.hint">
              A name you will recognise, such as the device it lives on.
            </Trans>
          </FieldDescription>
        </Field>
      </DialogPanel>
      <DialogFooter>
        <Button variant="ghost" disabled={pending} onClick={onClose}>
          <Trans id="security.cancel">Cancel</Trans>
        </Button>
        <Button type="submit" pending={pending}>
          <Trans id="security.passkeys.rename.save">Save name</Trans>
        </Button>
      </DialogFooter>
    </form>
  );
}
