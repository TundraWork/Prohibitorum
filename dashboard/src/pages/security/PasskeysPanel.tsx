import {
  Alert,
  AlertDialog,
  Chip,
  Description,
  Input,
  Label,
  TextField,
  Tooltip,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Fingerprint, KeyRound, Pencil, Trash2 } from "lucide-react";
import { useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  addCredentialMutationOptions,
  deleteCredentialMutationOptions,
  renameCredentialMutationOptions,
} from "@/api/mutations";
import { credentialsQueryOptions } from "@/api/queries";
import { Button } from "@/components/custom/Button";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
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
        }).format(new Date(value));

  const actions = (credential: Credential) => {
    const removeButton = (
      <Button
        isIconOnly
        size="sm"
        variant="danger-soft"
        aria-label={t({
          id: "security.passkeys.delete",
          message: "Remove",
        })}
        isDisabled={lastOne}
        onPress={() => setDeleting(credential)}
      >
        <Trash2 size={16} aria-hidden="true" />
      </Button>
    );
    return (
      <>
        <Button
          isIconOnly
          size="sm"
          variant="tertiary"
          aria-label={t({
            id: "security.passkeys.rename",
            message: "Rename",
          })}
          onPress={() =>
            setRenaming({
              credential,
              nickname: credential.nickname ?? "",
            })
          }
        >
          <Pencil size={16} aria-hidden="true" />
        </Button>
        {/* A disabled button emits no hover or focus, so the tooltip
            listens on the trigger wrapper instead. */}
        {lastOne ? (
          <Tooltip delay={0}>
            <Tooltip.Trigger>{removeButton}</Tooltip.Trigger>
            <Tooltip.Content>
              <Trans id="security.passkeys.last_one_tooltip">
                This is your only passkey, so it cannot be removed.
              </Trans>
            </Tooltip.Content>
          </Tooltip>
        ) : (
          removeButton
        )}
      </>
    );
  };

  return (
    <>
      <div className="flex flex-col gap-4">
        <div className="flex flex-wrap items-center gap-4">
          <Button isPending={add.isPending} onPress={startAdd}>
            <Trans id="security.passkeys.add">Add a passkey</Trans>
          </Button>
        </div>

        {error !== null && (
          <Alert status="danger" role="alert">
            <Alert.Indicator />
            <Alert.Content>
              <Alert.Title>{t(describeError(error))}</Alert.Title>
            </Alert.Content>
          </Alert>
        )}

        <ItemList
          label={t({ id: "security.passkeys.table", message: "Passkeys" })}
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
            />
          }
        >
          {rows.map((credential) => {
            const lastUsed = format(credential.lastUsedAt);
            return (
              <ItemListRow
                key={credential.id}
                icon={<KeyRound size={18} aria-hidden="true" />}
                title={
                  credential.nickname || (
                    <span className="text-muted">
                      <Trans id="security.passkeys.unnamed">
                        Unnamed passkey
                      </Trans>
                    </span>
                  )
                }
                badges={
                  <Chip size="sm" variant="soft">
                    {credential.backupState ? (
                      <Trans id="security.passkeys.synced">Synced</Trans>
                    ) : (
                      <Trans id="security.passkeys.device_bound">
                        This device only
                      </Trans>
                    )}
                  </Chip>
                }
                details={[
                  <Trans key="added" id="security.passkeys.detail.added">
                    Added {format(credential.createdAt)}
                  </Trans>,
                  lastUsed === null ? (
                    <Trans key="used" id="security.passkeys.never_used">
                      Not used yet
                    </Trans>
                  ) : (
                    <Trans key="used" id="security.passkeys.detail.lastUsed">
                      Last used {lastUsed}
                    </Trans>
                  ),
                  <span key="suffix" className="font-mono">
                    …{credential.credentialIdSuffix}
                  </span>,
                ]}
                actions={actions(credential)}
              />
            );
          })}
        </ItemList>
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
                  <Input autoComplete="off" variant="secondary" />
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
