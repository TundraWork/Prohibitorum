import {
  AlertDialog,
  Button,
  Description,
  Input,
  Label,
  TextField,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Fingerprint } from "lucide-react";
import { useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  addCredentialMutationOptions,
  deleteCredentialMutationOptions,
  renameCredentialMutationOptions,
} from "@/api/mutations";
import { credentialsQueryOptions } from "@/api/queries";
import { DataTable, type TableColumn } from "@/components/custom/DataTable";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { TableEmptyState } from "@/components/custom/TableEmptyState";

type Credential = components["schemas"]["CredentialView"];

/**
 * Passkeys on the account. Adding one runs the full WebAuthn registration
 * ceremony, which the server refuses without a fresh sudo window, so the sudo
 * prompt appears first — the user should understand why the browser is about to
 * ask for a passkey before it does.
 */
export function PasskeysPanel() {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const credentials = useQuery(credentialsQueryOptions());
  const [error, setError] = useState<unknown>(null);
  const [renaming, setRenaming] = useState<{
    credential: Credential;
    nickname: string;
  } | null>(null);
  const [deleting, setDeleting] = useState<Credential | null>(null);

  const add = useMutation(addCredentialMutationOptions(queryClient));
  const rename = useMutation(renameCredentialMutationOptions(queryClient));
  const remove = useMutation(deleteCredentialMutationOptions(queryClient));

  // The header action and the empty state both start the same registration.
  const startAdd = () => {
    setError(null);
    add.mutate(undefined, {
      onError: (failure) => {
        // A browser-level cancellation is a decision, not a failure:
        // there is nothing to report and nothing was changed.
        if (!isCancellation(failure)) setError(failure);
      },
    });
  };

  const rows = credentials.data ?? [];
  // The server rejects removing the last passkey; disable it here so the
  // reason is visible before the attempt, rather than only as an error.
  const lastOne = rows.length <= 1;

  const format = (value?: string) =>
    value === undefined
      ? null
      : new Intl.DateTimeFormat(i18n.locale, {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(new Date(value));

  const columns: readonly TableColumn<Credential>[] = [
    {
      id: "nickname",
      header: <Trans id="security.passkeys.column.name">Name</Trans>,
      cell: (credential) =>
        credential.nickname ? (
          <span className="wrap-anywhere font-medium">
            {credential.nickname}
          </span>
        ) : (
          <span className="text-muted">
            <Trans id="security.passkeys.unnamed">Unnamed passkey</Trans>
          </span>
        ),
    },
    {
      id: "createdAt",
      header: <Trans id="security.passkeys.column.created">Added</Trans>,
      cell: (credential) => format(credential.createdAt),
    },
    {
      id: "lastUsedAt",
      header: <Trans id="security.passkeys.column.lastUsed">Last used</Trans>,
      cell: (credential) =>
        format(credential.lastUsedAt) ?? (
          <span className="text-muted">
            <Trans id="security.passkeys.never_used">Not used yet</Trans>
          </span>
        ),
    },
    {
      id: "backupState",
      header: <Trans id="security.passkeys.column.synced">Synced</Trans>,
      cell: (credential) =>
        credential.backupState ? (
          <Trans id="security.passkeys.synced">Synced</Trans>
        ) : (
          <Trans id="security.passkeys.device_bound">This device only</Trans>
        ),
    },
    {
      id: "transports",
      header: <Trans id="security.passkeys.column.transport">Transport</Trans>,
      cell: (credential) =>
        credential.transports && credential.transports.length > 0 ? (
          <span className="wrap-anywhere">
            {credential.transports.join(", ")}
          </span>
        ) : (
          "—"
        ),
    },
    {
      id: "suffix",
      header: <Trans id="security.passkeys.column.id">Ends with</Trans>,
      cell: (credential) => (
        <span className="font-mono">{credential.credentialIdSuffix}</span>
      ),
    },
    {
      id: "actions",
      header: <Trans id="security.column.actions">Actions</Trans>,
      cell: (credential) => (
        <div className="flex flex-wrap justify-end gap-2">
          <Button
            size="sm"
            variant="secondary"
            onPress={() =>
              setRenaming({
                credential,
                nickname: credential.nickname ?? "",
              })
            }
          >
            <Trans id="security.passkeys.rename">Rename</Trans>
          </Button>
          <Button
            size="sm"
            variant="danger-soft"
            isDisabled={lastOne}
            onPress={() => setDeleting(credential)}
          >
            <Trans id="security.passkeys.delete">Remove</Trans>
          </Button>
        </div>
      ),
      align: "end",
    },
  ];

  return (
    <>
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center justify-end gap-4">
          <Button isPending={add.isPending} onPress={startAdd}>
            <Trans id="security.passkeys.add">Add a passkey</Trans>
          </Button>
        </div>

        {lastOne && rows.length === 1 && (
          <Description>
            <Trans id="security.passkeys.last_one">
              This is your only passkey, so it cannot be removed. Add another
              one first.
            </Trans>
          </Description>
        )}

        {error !== null && (
          <SurfaceAlert status="danger" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>{t(describeError(error))}</SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        <DataTable
          label={t({ id: "security.passkeys.table", message: "Passkeys" })}
          columns={columns}
          rows={rows}
          rowId={(credential) => credential.id}
          loading={credentials.isPending}
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
              hint={
                <Trans id="security.passkeys.empty.hint">
                  A passkey signs you in without typing a password.
                </Trans>
              }
              action={
                <Button size="sm" isPending={add.isPending} onPress={startAdd}>
                  <Trans id="security.passkeys.add">Add a passkey</Trans>
                </Button>
              }
            />
          }
        />
      </div>

      <AlertDialog
        isOpen={deleting !== null}
        onOpenChange={(open) => !open && setDeleting(null)}
      >
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>
                  <Trans id="security.passkeys.delete.title">
                    Remove this passkey?
                  </Trans>
                </AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>
                <p>
                  <Trans id="security.passkeys.delete.body">
                    This passkey will stop working for sign-in right away. Any
                    other way you have to sign in is unaffected.
                  </Trans>
                </p>
              </AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  onPress={() => setDeleting(null)}
                  isDisabled={remove.isPending}
                >
                  <Trans id="security.keep">Keep it</Trans>
                </Button>
                <Button
                  variant="danger"
                  isPending={remove.isPending}
                  onPress={() => {
                    if (!deleting) return;
                    remove.mutate(deleting.id, {
                      onSuccess: () => setDeleting(null),
                      onError: (failure) => {
                        setError(failure);
                        setDeleting(null);
                      },
                    });
                  }}
                >
                  <Trans id="security.passkeys.delete.confirm">Remove</Trans>
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>

      <AlertDialog
        isOpen={renaming !== null}
        onOpenChange={(open) => !open && setRenaming(null)}
      >
        <AlertDialog.Backdrop>
          <AlertDialog.Container placement="center" size="md">
            <AlertDialog.Dialog>
              <AlertDialog.Header>
                <AlertDialog.Heading>
                  <Trans id="security.passkeys.rename.title">
                    Rename this passkey
                  </Trans>
                </AlertDialog.Heading>
              </AlertDialog.Header>
              <AlertDialog.Body>
                <TextField
                  value={renaming?.nickname ?? ""}
                  onChange={(value) =>
                    setRenaming((current) =>
                      current ? { ...current, nickname: value } : current,
                    )
                  }
                >
                  <Label>
                    <Trans id="security.passkeys.rename.label">Name</Trans>
                  </Label>
                  <Input autoComplete="off" />
                  <Description>
                    <Trans id="security.passkeys.rename.hint">
                      A name you will recognise, such as the device it lives on.
                    </Trans>
                  </Description>
                </TextField>
              </AlertDialog.Body>
              <AlertDialog.Footer>
                <Button
                  variant="secondary"
                  onPress={() => setRenaming(null)}
                  isDisabled={rename.isPending}
                >
                  <Trans id="security.cancel">Cancel</Trans>
                </Button>
                <Button
                  isPending={rename.isPending}
                  onPress={() => {
                    if (!renaming) return;
                    rename.mutate(
                      {
                        id: renaming.credential.id,
                        nickname: renaming.nickname,
                      },
                      {
                        onSuccess: () => setRenaming(null),
                        onError: (failure) => {
                          setError(failure);
                          setRenaming(null);
                        },
                      },
                    );
                  }}
                >
                  <Trans id="security.passkeys.rename.save">Save name</Trans>
                </Button>
              </AlertDialog.Footer>
            </AlertDialog.Dialog>
          </AlertDialog.Container>
        </AlertDialog.Backdrop>
      </AlertDialog>
    </>
  );
}
