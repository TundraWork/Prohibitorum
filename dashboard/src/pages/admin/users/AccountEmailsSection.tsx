import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { Mail, ShieldCheck, Trash2 } from "lucide-react";
import { useState } from "react";
import { ApiError } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  addAccountEmailMutationOptions,
  deleteAccountEmailMutationOptions,
  setAccountEmailPrimaryMutationOptions,
  verifyAccountEmailMutationOptions,
} from "@/api/mutations";
import { accountEmailsQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
import { notifySuccess } from "@/components/custom/AppNotifications";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { RowMenu } from "@/components/custom/RowMenu";
import { AsyncSection, Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import { Badge } from "@/components/ui/badge";
import { Checkbox } from "@/components/ui/checkbox";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { MenuItem, MenuSeparator } from "@/components/ui/menu";

type EmailAddress = components["schemas"]["EmailAddressView"];

const addressTakenError = msg({
  id: "admin.user.emails.error.taken",
  message: "That address is already verified by another account.",
});
const addressInvalidError = msg({
  id: "admin.user.emails.error.invalid",
  message: "That is not a valid email address.",
});
const primaryToast = msg({
  id: "admin.user.emails.primary_toast",
  message: "{address} is now the user's primary address",
});

/**
 * The section's heading, drawn by the panel and by the boundary that waits for
 * its read, so the two cannot word it differently.
 */
export const accountEmailsSection = {
  id: "account-emails",
  icon: <Mail />,
  title: <Trans id="admin.user.emails.title">Email addresses</Trans>,
  description: (
    <Trans id="admin.user.emails.description">
      The addresses on this account. Apps see the primary one; the others are
      never sent to them.
    </Trans>
  ),
};

/**
 * An administrator's view of one account's addresses: the same rows the member
 * sees, plus what only an administrator can do — confirm an address as
 * verified without a code, choose which one is primary, and take any address
 * off the account, the primary included.
 */
export function AccountEmailsSection({ accountId }: { accountId: number }) {
  return (
    <AsyncSection resetKey="account-emails" {...accountEmailsSection}>
      <AccountEmailsList accountId={accountId} />
    </AsyncSection>
  );
}

function AccountEmailsList({ accountId }: { accountId: number }) {
  const { t } = useLingui();
  const { data } = useSuspenseQuery(
    quietQuery(accountEmailsQueryOptions(accountId)),
  );
  const [adding, setAdding] = useState(false);
  const [asserting, setAsserting] = useState<EmailAddress | null>(null);
  const [makingPrimary, setMakingPrimary] = useState<EmailAddress | null>(null);
  const [removing, setRemoving] = useState<EmailAddress | null>(null);
  const queryClient = useQueryClient();
  const assert = useMutation(verifyAccountEmailMutationOptions(queryClient));
  const setPrimary = useMutation(
    setAccountEmailPrimaryMutationOptions(queryClient),
  );
  const remove = useMutation(deleteAccountEmailMutationOptions(queryClient));

  const emails = data.emails ?? [];
  const hasPrimary = emails.some((email) => email.primary);
  // Deleting a primary hands the role to another verified address, so the
  // confirmation names which one takes over — or that none will.
  const nextPrimary = emails.find((email) => !email.primary && email.verified);

  return (
    <>
      <Section
        {...accountEmailsSection}
        action={
          <Button variant="outline" onClick={() => setAdding(true)}>
            <Trans id="admin.user.emails.add">Add address</Trans>
          </Button>
        }
      >
        {emails.length > 0 && !hasPrimary && (
          <SurfaceAlert status="warning">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                <Trans id="admin.user.emails.no_primary">
                  This account has no primary address. Apps that need one may
                  not let the user sign in; verify an address to make it the
                  primary.
                </Trans>
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        <ItemList
          label={t({
            id: "admin.user.emails.table",
            message: "Email addresses",
          })}
          empty={
            <TableEmptyState
              icon={<Mail size={18} strokeWidth={1.75} aria-hidden="true" />}
              title={
                <Trans id="admin.user.emails.empty">No addresses yet</Trans>
              }
              description={
                <Trans id="admin.user.emails.empty.hint">
                  Some apps need an address before they let the user sign in.
                </Trans>
              }
            />
          }
        >
          {emails.map((email) => (
            <AccountEmailRow
              key={email.id}
              email={email}
              onAssert={() => setAsserting(email)}
              onMakePrimary={() => setMakingPrimary(email)}
              onRemove={() => setRemoving(email)}
            />
          ))}
        </ItemList>
      </Section>

      <AddAccountEmailDialog
        open={adding}
        accountId={accountId}
        onClose={() => setAdding(false)}
      />

      <ConfirmDialog
        isOpen={asserting !== null}
        onOpenChange={(open) => {
          if (!open && !assert.isPending) setAsserting(null);
        }}
        status="accent"
        title={
          <Trans id="admin.user.emails.assert.title">
            Confirm this address belongs to the user?
          </Trans>
        }
        subject={asserting?.address}
        body={
          <p>
            <Trans id="admin.user.emails.assert.body">
              Apps will treat it as a verified address.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.user.emails.assert.action">
            Confirm as verified
          </Trans>
        }
        isPending={assert.isPending}
        onConfirm={() => {
          if (asserting === null) return;
          assert.mutate(
            { id: accountId, emailId: asserting.id },
            { onSuccess: () => setAsserting(null) },
          );
        }}
      />

      <ConfirmDialog
        isOpen={makingPrimary !== null}
        onOpenChange={(open) => {
          if (!open && !setPrimary.isPending) setMakingPrimary(null);
        }}
        status="warning"
        title={
          <Trans id="admin.user.emails.primary.title">
            Make this the primary address?
          </Trans>
        }
        subject={makingPrimary?.address}
        body={
          <p>
            <Trans id="admin.user.emails.primary.body">
              Apps will see this address the next time the user signs in. An app
              that knows users by their address may take them for someone else.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.user.emails.make_primary">Make primary</Trans>
        }
        isPending={setPrimary.isPending}
        onConfirm={() => {
          if (makingPrimary === null) return;
          const target = makingPrimary;
          setPrimary.mutate(
            { id: accountId, emailId: target.id },
            {
              onSuccess: () => {
                setMakingPrimary(null);
                notifySuccess({
                  ...primaryToast,
                  values: { address: target.address },
                });
              },
            },
          );
        }}
      />

      <ConfirmDialog
        isOpen={removing !== null}
        onOpenChange={(open) => {
          if (!open && !remove.isPending) setRemoving(null);
        }}
        status="danger"
        title={
          <Trans id="admin.user.emails.delete.title">Remove address?</Trans>
        }
        subject={removing?.address}
        body={
          removing?.primary ? (
            nextPrimary ? (
              <p>
                <Trans id="admin.user.emails.delete.fallback">
                  {nextPrimary.address} will become the new primary address.
                </Trans>
              </p>
            ) : (
              <p>
                <Trans id="admin.user.emails.delete.no_fallback">
                  The user will have no primary address. Apps that need one may
                  not let them sign in.
                </Trans>
              </p>
            )
          ) : (
            <p>
              <Trans id="admin.user.emails.delete.body">
                To use this address again, add it back and verify it once more.
              </Trans>
            </p>
          )
        }
        confirmLabel={
          <Trans id="admin.user.emails.delete.action">Remove</Trans>
        }
        isPending={remove.isPending}
        onConfirm={() => {
          if (removing === null) return;
          remove.mutate(
            { id: accountId, emailId: removing.id },
            { onSuccess: () => setRemoving(null) },
          );
        }}
      />
    </>
  );
}

function AccountEmailRow({
  email,
  onAssert,
  onMakePrimary,
  onRemove,
}: {
  email: EmailAddress;
  onAssert: () => void;
  onMakePrimary: () => void;
  onRemove: () => void;
}) {
  const { t } = useLingui();
  const address = email.address;

  return (
    <ItemListRow
      icon={<Mail aria-hidden="true" className="size-4" />}
      title={address}
      badge={
        email.primary ? (
          <Badge variant="outline">
            <Trans id="admin.user.emails.primary">Primary</Trans>
          </Badge>
        ) : undefined
      }
      meta={
        email.verified ? (
          <span className="flex items-center gap-1.5">
            <span
              aria-hidden="true"
              className="inline-block size-2 rounded-full bg-success"
            />
            <Trans id="admin.user.emails.status.verified">Verified</Trans>
          </span>
        ) : (
          <span className="flex items-center gap-1.5">
            <span
              aria-hidden="true"
              className="inline-block size-2 rounded-full bg-warning"
            />
            <Trans id="admin.user.emails.status.unverified">Not verified</Trans>
          </span>
        )
      }
      details={[<AdminVerifiedDetail key="detail" email={email} />]}
      stackActions
      actions={
        <RowMenu
          label={t({
            id: "admin.user.emails.more",
            message: `More actions for ${address}`,
          })}
        >
          {!email.verified && (
            <MenuItem onClick={onAssert}>
              <ShieldCheck />
              <Trans id="admin.user.emails.assert.action">
                Confirm as verified
              </Trans>
            </MenuItem>
          )}
          {email.verified && !email.primary && (
            <MenuItem onClick={onMakePrimary}>
              <ShieldCheck />
              <Trans id="admin.user.emails.make_primary">Make primary</Trans>
            </MenuItem>
          )}
          <MenuSeparator />
          <MenuItem variant="destructive" onClick={onRemove}>
            <Trash2 />
            <Trans id="admin.user.emails.delete.action">Remove</Trans>
          </MenuItem>
        </RowMenu>
      }
    />
  );
}

/** Who confirmed the address and when, with the operator named when there was one. */
function AdminVerifiedDetail({ email }: { email: EmailAddress }) {
  if (!email.verified) {
    return (
      <Trans id="admin.user.emails.added">
        Added <RelativeTime value={email.createdAt} />
      </Trans>
    );
  }
  if (email.verifiedAt === undefined) return null;
  const when = <RelativeTime value={email.verifiedAt} />;
  if (email.verifiedVia === "admin") {
    const operator = email.verifiedBy;
    return operator ? (
      <Trans id="admin.user.emails.verified_by_named">
        Verified {when} by {operator}
      </Trans>
    ) : (
      <Trans id="admin.user.emails.verified_by_admin">
        Verified {when} by an administrator
      </Trans>
    );
  }
  if (email.verifiedVia === "upstream" && email.verifiedProvider) {
    const provider = email.verifiedProvider;
    return (
      <Trans id="admin.user.emails.verified_by_provider">
        Verified {when} by {provider}
      </Trans>
    );
  }
  return (
    <Trans id="admin.user.emails.verified_by_code">
      Verified {when} with a code
    </Trans>
  );
}

/**
 * The one-step add: an address, and what the administrator vouches for. The
 * primary checkbox waits on the verified one because the server refuses an
 * unverified primary — the help line says so rather than leaving the disabled
 * box to explain itself.
 */
function AddAccountEmailDialog({
  open,
  accountId,
  onClose,
}: {
  open: boolean;
  accountId: number;
  onClose: () => void;
}) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const [address, setAddress] = useState("");
  const [verified, setVerified] = useState(false);
  const [primary, setPrimary] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const add = useMutation(addAccountEmailMutationOptions(queryClient));

  const close = () => {
    if (add.isPending) return;
    setError(null);
    setAddress("");
    setVerified(false);
    setPrimary(false);
    onClose();
  };

  const submit = async () => {
    setError(null);
    try {
      await add.mutateAsync({
        id: accountId,
        address: address.trim(),
        verified: verified || undefined,
        primary: (verified && primary) || undefined,
      });
      close();
    } catch (failure) {
      if (failure instanceof ApiError) {
        if (failure.code === "email_taken") {
          setError(i18n._(addressTakenError));
          return;
        }
        if (failure.code === "email_invalid") {
          setError(i18n._(addressInvalidError));
          return;
        }
      }
      throw failure;
    }
  };

  const verifiedHelp = t({
    id: "admin.user.emails.verified_help",
    message: "Check this only if you have confirmed the address is the user's.",
  });
  const primaryHelp = t({
    id: "admin.user.emails.primary_help",
    message: "Only a verified address can be the primary one.",
  });

  return (
    <Dialog open={open} onOpenChange={(next) => !next && close()}>
      <DialogPopup className="sm:max-w-md">
        <form
          className="flex min-h-0 flex-1 flex-col"
          aria-label={t({
            id: "admin.user.emails.add.title",
            message: "Add an email address",
          })}
          onSubmit={(event) => {
            event.preventDefault();
            if (!add.isPending && address.trim() !== "") void submit();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              <Trans id="admin.user.emails.add.title">
                Add an email address
              </Trans>
            </DialogTitle>
          </DialogHeader>
          <DialogPanel className="flex flex-col gap-4">
            <Field invalid={error !== null}>
              <FieldLabel>
                <Trans id="admin.user.emails.address">Address</Trans>
              </FieldLabel>
              <Input
                type="email"
                autoComplete="off"
                value={address}
                onValueChange={(value) => {
                  setError(null);
                  setAddress(value);
                }}
              />
              {error !== null && <FieldError>{error}</FieldError>}
            </Field>
            <div className="flex flex-col gap-3">
              <Label className="gap-2.5 text-sm font-normal">
                <Checkbox
                  checked={verified}
                  onCheckedChange={(next) => {
                    setVerified(next === true);
                    if (next !== true) setPrimary(false);
                  }}
                />
                <Trans id="admin.user.emails.mark_verified">
                  Confirm as verified
                </Trans>
              </Label>
              <p className="text-muted-foreground text-caption -mt-1">
                {verifiedHelp}
              </p>
              <Label
                className="gap-2.5 text-sm font-normal"
                data-disabled={verified ? undefined : "true"}
              >
                <Checkbox
                  checked={primary}
                  disabled={!verified}
                  onCheckedChange={(next) => setPrimary(next === true)}
                />
                <Trans id="admin.user.emails.mark_primary">
                  Make it the primary address
                </Trans>
              </Label>
              {!verified && (
                <p className="text-muted-foreground text-caption -mt-1">
                  {primaryHelp}
                </p>
              )}
            </div>
          </DialogPanel>
          <DialogFooter>
            <Button variant="ghost" disabled={add.isPending} onClick={close}>
              <Trans id="admin.user.emails.cancel">Cancel</Trans>
            </Button>
            <Button
              type="submit"
              pending={add.isPending}
              disabled={address.trim() === ""}
            >
              <Trans id="admin.user.emails.add.short">Add</Trans>
            </Button>
          </DialogFooter>
        </form>
      </DialogPopup>
    </Dialog>
  );
}
