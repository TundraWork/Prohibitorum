import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { Mail, MailCheck, ShieldCheck, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { ApiError, isCancellation, retryAfterSeconds } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  addMyEmailMutationOptions,
  deleteMyEmailMutationOptions,
  sendMyEmailCodeMutationOptions,
  setMyEmailPrimaryMutationOptions,
  verifyMyEmailMutationOptions,
} from "@/api/mutations";
import { myEmailsQueryOptions } from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
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
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { MenuItem, MenuSeparator } from "@/components/ui/menu";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { applyServerError, ServerFormError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

export type EmailAddress = components["schemas"]["EmailAddressView"];

/** The verification code the server mails is always six digits. */
const codeLength = 6;

const addLimitError = msg({
  id: "profile.emails.error.limit",
  message:
    "You can add up to {limit} addresses. Remove one before adding another.",
});
const addExistsError = msg({
  id: "profile.emails.error.exists",
  message: "This address is already in your list.",
});
const addUnavailableError = msg({
  id: "profile.emails.error.unavailable",
  message: "This address cannot be used.",
});
const codeAttemptsError = msg({
  id: "profile.emails.code_attempts",
  message: "The code is incorrect. {attempts} attempts left.",
});
const codeWrongError = msg({
  id: "profile.emails.code_wrong",
  message: "The code is incorrect.",
});
const codeExpiredError = msg({
  id: "profile.emails.code_expired",
  message: "The code has expired. Send a new one.",
});
const sendFailedError = msg({
  id: "profile.emails.error.send_failed",
  message: "The code could not be sent.",
});
const verifiedToast = msg({
  id: "profile.emails.verified_toast",
  message: "Verified {address}",
});
const verifiedPrimaryToast = msg({
  id: "profile.emails.verified_toast.primary",
  message: "Verified {address} and set it as your primary address.",
});
const primaryToast = msg({
  id: "profile.emails.primary_toast",
  message: "Set {address} as your primary address",
});
/** The limit refusal needs the count, which the form error path cannot close over. */
const addLimitErrorWith = (limit: number) => ({
  ...addLimitError,
  values: { limit },
});
const addedToast = msg({
  id: "profile.emails.added_toast",
  message: "Added {address}. Waiting for an administrator to confirm it",
});
const removedToast = msg({
  id: "profile.emails.removed_toast",
  message: "Removed {address}",
});

/**
 * The section's heading, drawn by the panel and by the boundary that waits for
 * its read, so the two cannot word it differently.
 */
export const emailsSection = {
  id: "profile-section-emails",
  icon: <Mail />,
  title: <Trans id="profile.emails.title">Email addresses</Trans>,
  description: (
    <Trans id="profile.emails.description">
      If you authorize an application, your primary address is shared with it.
    </Trans>
  ),
};

/**
 * The account's email addresses, with the one applications will see.
 *
 * The primary is what the outside world gets, and changing it can make an app
 * that knows someone by their address see a stranger, so that step waits for a
 * confirmation; removal does too, since a removed address has to be re-verified
 * before it can come back. Verifying a fresh address is a code the server
 * mails when it can: an instance without SMTP still accepts the address and
 * waits for an administrator to confirm it instead.
 */
export function EmailsPanel() {
  return (
    <AsyncSection resetKey="emails" {...emailsSection}>
      <EmailsList />
    </AsyncSection>
  );
}

function EmailsList() {
  const { t } = useLingui();
  const { data } = useSuspenseQuery(quietQuery(myEmailsQueryOptions()));
  const [adding, setAdding] = useState(false);
  // The address being verified, from the add flow or from a row's menu.
  const [verifying, setVerifying] = useState<EmailAddress | null>(null);
  const [makingPrimary, setMakingPrimary] = useState<EmailAddress | null>(null);
  const [removing, setRemoving] = useState<EmailAddress | null>(null);

  const emails = data.emails ?? [];
  const hasPrimary = emails.some((email) => email.primary);
  const atCap = emails.length >= data.limit;

  return (
    <>
      <Section
        {...emailsSection}
        action={
          atCap ? (
            // A button kept focusable but inert still emits the hover and
            // focus a tooltip answers, which a `disabled` one would not.
            <Tooltip>
              <TooltipTrigger
                render={
                  <Button
                    variant="outline"
                    aria-disabled="true"
                    aria-label={t({
                      id: "profile.emails.add",
                      message: "Add address",
                    })}
                  />
                }
              >
                <Trans id="profile.emails.add">Add address</Trans>
              </TooltipTrigger>
              <TooltipPopup>
                <Trans id="profile.emails.cap">
                  You can add up to {data.limit} addresses. Remove one before
                  adding another.
                </Trans>
              </TooltipPopup>
            </Tooltip>
          ) : (
            <Button variant="outline" onClick={() => setAdding(true)}>
              <Trans id="profile.emails.add">Add address</Trans>
            </Button>
          )
        }
      >
        {!data.deliveryAvailable ? (
          // Without mail there is nothing the reader can verify themselves, so
          // this stands in for the no-primary warning rather than stacking on
          // it: the fix is the same administrator either way.
          <SurfaceAlert status="accent">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                <Trans id="profile.emails.no_delivery">
                  This instance does not send email. An address you add is
                  marked as verified only after an administrator confirms it.
                </Trans>
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        ) : emails.length > 0 && !hasPrimary ? (
          <SurfaceAlert status="warning">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>
                <Trans id="profile.emails.no_primary">
                  You have no primary address, so some applications may not let
                  you sign in. The first address you verify becomes your primary
                  address.
                </Trans>
              </SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        ) : null}

        <ItemList
          label={t({ id: "profile.emails.table", message: "Email addresses" })}
          empty={
            <TableEmptyState
              icon={<Mail size={18} strokeWidth={1.75} aria-hidden="true" />}
              title={<Trans id="profile.emails.empty">No addresses yet</Trans>}
              description={
                <Trans id="profile.emails.empty.hint">
                  Some applications require an email address to sign in.
                </Trans>
              }
            />
          }
        >
          {emails.map((email) => (
            <EmailRow
              key={email.id}
              email={email}
              deliveryAvailable={data.deliveryAvailable}
              onVerify={() => setVerifying(email)}
              onMakePrimary={() => setMakingPrimary(email)}
              onRemove={() => setRemoving(email)}
            />
          ))}
        </ItemList>
      </Section>

      <AddEmailDialog
        open={adding}
        deliveryAvailable={data.deliveryAvailable}
        limit={data.limit}
        onClose={() => setAdding(false)}
        onVerify={setVerifying}
      />

      <VerifyEmailDialog
        address={verifying}
        onClose={() => setVerifying(null)}
      />

      <SetPrimaryDialog
        address={makingPrimary}
        onClose={() => setMakingPrimary(null)}
      />
      <RemoveDialog address={removing} onClose={() => setRemoving(null)} />
    </>
  );
}

function EmailRow({
  email,
  deliveryAvailable,
  onVerify,
  onMakePrimary,
  onRemove,
}: {
  email: EmailAddress;
  deliveryAvailable: boolean;
  onVerify: () => void;
  onMakePrimary: () => void;
  onRemove: () => void;
}) {
  const { t } = useLingui();
  const address = email.address;

  // The unverified address's next step rides in the menu with the rest:
  // Verify when a code can be sent, or the standing note when it cannot.
  const waitingForAdmin = !email.verified && !deliveryAvailable;
  const canVerify = !email.verified && deliveryAvailable;

  return (
    <ItemListRow
      icon={<Mail aria-hidden="true" className="size-4" />}
      title={address}
      badge={
        email.primary ? (
          <Badge variant="outline">
            <Trans id="profile.emails.primary">Primary</Trans>
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
            <Trans id="profile.emails.status.verified">Verified</Trans>
          </span>
        ) : (
          <span className="flex items-center gap-1.5">
            <span
              aria-hidden="true"
              className="inline-block size-2 rounded-full bg-warning"
            />
            <Trans id="profile.emails.status.unverified">Not verified</Trans>
          </span>
        )
      }
      details={[<VerifiedDetail key="detail" email={email} />]}
      stackActions
      actions={
        email.primary ? undefined : (
          <RowMenu
            label={t({
              id: "profile.emails.more",
              message: `More actions for ${address}`,
            })}
          >
            {canVerify && (
              <MenuItem onClick={onVerify}>
                <MailCheck />
                <Trans id="profile.emails.verify">Verify</Trans>
              </MenuItem>
            )}
            {email.verified && (
              <MenuItem onClick={onMakePrimary}>
                <ShieldCheck />
                <Trans id="profile.emails.make_primary">Make primary</Trans>
              </MenuItem>
            )}
            {/* The divider separates a state-changing action from the
                destructive one; a menu whose only action is Remove draws
                no divider above it. */}
            {(canVerify || email.verified) && <MenuSeparator />}
            <MenuItem variant="destructive" onClick={onRemove}>
              <Trash2 />
              <Trans id="profile.emails.delete.action">Remove</Trans>
            </MenuItem>
          </RowMenu>
        )
      }
    >
      {waitingForAdmin && (
        <p className="text-muted-foreground text-caption">
          <Trans id="profile.emails.awaiting_admin">
            Waiting for an administrator
          </Trans>
        </p>
      )}
    </ItemListRow>
  );
}

/**
 * How the address came to be trusted, or where it stands: who confirmed it and
 * when it was added, or — without mail — that an administrator is the one who
 * can confirm it.
 */
function VerifiedDetail({ email }: { email: EmailAddress }) {
  if (!email.verified) {
    return (
      <Trans id="profile.emails.added">
        Added <RelativeTime value={email.createdAt} />
      </Trans>
    );
  }
  if (email.verifiedAt === undefined) return null;
  const when = <RelativeTime value={email.verifiedAt} />;
  if (email.verifiedVia === "admin") {
    return (
      <Trans id="profile.emails.verified_by_admin">
        Verified {when} by an administrator
      </Trans>
    );
  }
  if (email.verifiedVia === "upstream" && email.verifiedProvider) {
    const provider = email.verifiedProvider;
    return (
      <Trans id="profile.emails.verified_by_provider">
        Verified {when} by {provider}
      </Trans>
    );
  }
  return (
    <Trans id="profile.emails.verified_by_code">
      Verified {when} with an email code
    </Trans>
  );
}

/**
 * Step one of adding an address: the address itself. With mail configured the
 * button sends the code and hands over to the verify step; without it there is
 * nothing to send, so the dialog ends here and the row waits for an
 * administrator instead.
 */
function AddEmailDialog({
  open,
  deliveryAvailable,
  limit,
  onClose,
  onVerify,
}: {
  open: boolean;
  deliveryAvailable: boolean;
  limit: number;
  onClose: () => void;
  onVerify: (email: EmailAddress) => void;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  // Every refusal lands in the form's field, so the toast stays silent.
  const add = useMutation(
    withInlineReport(addMyEmailMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: { address: "" },
    onSubmit: async ({ value }) => {
      try {
        const created = await add.mutateAsync(value.address.trim());
        form.reset();
        if (deliveryAvailable) {
          // Close this dialog before the code dialog opens, so the two never
          // stack (review B7).
          onClose();
          onVerify(created);
        } else {
          notifySuccess({
            ...addedToast,
            values: { address: created.address },
          });
          onClose();
        }
      } catch (failure) {
        if (isCancellation(failure)) return;
        applyServerError(form, failure, {
          locations: {},
          codes: { email_invalid: "address" },
        });
        if (failure instanceof ApiError) {
          if (failure.code === "email_limit_reached") {
            form.setErrorMap({
              onSubmit: {
                fields: {
                  address: new ServerFormError(
                    failure,
                    addLimitErrorWith(limit),
                  ),
                },
              },
            });
            return;
          }
          if (failure.code === "email_exists") {
            form.setErrorMap({
              onSubmit: {
                fields: {
                  address: new ServerFormError(failure, addExistsError),
                },
              },
            });
            return;
          }
        }
      }
    },
  });

  const address = useStore(form.store, (state) => state.values.address);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const close = () => {
    if (submitting) return;
    form.reset();
    onClose();
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && close()}>
      <DialogPopup className="sm:max-w-md">
        <form.AppForm>
          <form.Form
            label={t({
              id: "profile.emails.add.title",
              message: "Add an email address",
            })}
            className="flex min-h-0 flex-1 flex-col"
          >
            <DialogHeader>
              <DialogTitle>
                <Trans id="profile.emails.add.title">
                  Add an email address
                </Trans>
              </DialogTitle>
            </DialogHeader>
            <DialogPanel>
              <form.AppField name="address">
                {(field) => (
                  <field.FormField
                    label={<Trans id="profile.emails.address">Address</Trans>}
                    type="email"
                    autoComplete="off"
                    autoFocus
                  />
                )}
              </form.AppField>
            </DialogPanel>
            <DialogFooter>
              <Button variant="ghost" disabled={submitting} onClick={close}>
                <Trans id="profile.cancel">Cancel</Trans>
              </Button>
              <Button
                type="submit"
                pending={submitting}
                disabled={address.trim() === ""}
              >
                {deliveryAvailable ? (
                  <Trans id="profile.emails.send_code">Send code</Trans>
                ) : (
                  <Trans id="profile.emails.add.short">Add</Trans>
                )}
              </Button>
            </DialogFooter>
          </form.Form>
        </form.AppForm>
      </DialogPopup>
    </Dialog>
  );
}

/**
 * Step two: the six digits. Opening the dialog is what sends the first code —
 * from the add flow's step change or from a row's Verify — and a re-send is
 * the footer button's job. The cooldown the server answers with (or a 429
 * that says one is still running) counts down on that button, so the reader
 * is never told a code failed to go out when it is merely on its way. A send
 * that really failed says so beside the field, with the button to retry it.
 * Six digits submit on their own; the footer button stays for a code the
 * reader had to leave the field for.
 */
function VerifyEmailDialog({
  address,
  onClose,
}: {
  address: EmailAddress | null;
  onClose: () => void;
}) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const send = useMutation(
    withInlineReport(sendMyEmailCodeMutationOptions(), true),
  );
  const verify = useMutation(
    withInlineReport(verifyMyEmailMutationOptions(queryClient), true),
  );
  const [error, setError] = useState<string | null>(null);
  const [deliveryFailed, setDeliveryFailed] = useState(false);
  const [cooldown, setCooldown] = useState(0);
  // Guards the auto-submit against firing twice for one code.
  const submitted = useRef("");
  const [startedFor, setStartedFor] = useState<number | null>(null);

  const form = useAppForm({
    defaultValues: { code: "" },
    onSubmit: async ({ value }) => {
      if (address === null || value.code.length !== codeLength) return;
      if (submitted.current === value.code) return;
      submitted.current = value.code;
      setError(null);
      try {
        const verified = await verify.mutateAsync({
          id: address.id,
          code: value.code,
        });
        onClose();
        const confirmed = verified.address;
        notifySuccess(
          verified.primary
            ? { ...verifiedPrimaryToast, values: { address: confirmed } }
            : { ...verifiedToast, values: { address: confirmed } },
        );
      } catch (failure) {
        submitted.current = "";
        if (failure instanceof ApiError) {
          if (failure.code === "email_code_invalid") {
            const attempts = failure.details?.attemptsRemaining;
            setError(
              typeof attempts === "number"
                ? i18n._({ ...codeAttemptsError, values: { attempts } })
                : i18n._(codeWrongError),
            );
            form.reset();
            return;
          }
          if (failure.code === "email_code_expired") {
            setError(i18n._(codeExpiredError));
            form.reset();
            return;
          }
          if (failure.code === "email_unavailable") {
            setError(i18n._(addUnavailableError));
            return;
          }
        }
        throw failure;
      }
    },
  });

  const code = useStore(form.store, (state) => state.values.code);

  // The cooldown ticks once a second while any of it is left.
  const cooling = cooldown > 0;
  useEffect(() => {
    if (!cooling) return;
    const timer = setInterval(
      () => setCooldown((seconds) => Math.max(0, seconds - 1)),
      1000,
    );
    return () => clearInterval(timer);
  }, [cooling]);

  // A complete code submits itself: the reader's last digit is the intent.
  useEffect(() => {
    if (code.length !== codeLength || verify.isPending) return;
    if (submitted.current === code) return;
    void form.handleSubmit();
  }, [code, verify.isPending, form]);

  const sendCode = () => {
    if (address === null) return;
    send.mutate(
      { id: address.id, locale: i18n.locale },
      {
        onSuccess: (out) => {
          setCooldown(out.resendAfterSeconds);
          setDeliveryFailed(false);
          setError(null);
          form.reset();
        },
        onError: (failure) => {
          if (failure instanceof ApiError) {
            if (failure.status === 429) {
              // A cooldown that is still running answers 429 with how long
              // it has left: that is the countdown, not a failure.
              const wait = retryAfterSeconds(failure);
              if (wait !== undefined) {
                setCooldown(wait);
                setDeliveryFailed(false);
                setError(null);
                return;
              }
            }
            if (failure.code === "email_delivery_failed") {
              // The address stands; only the letter failed. The reader can
              // resend without losing their place.
              setDeliveryFailed(true);
              return;
            }
          }
          setError(i18n._(sendFailedError));
        },
      },
    );
  };

  // Opening the dialog is what sends the first code: the row's Verify item
  // opens it and the add flow's step change is the same moment. The
  // dependency is the address id alone — a resend must not re-run this, and
  // the mutation/form objects are stable across renders.
  const openedFor = address?.id ?? null;
  // biome-ignore lint/correctness/useExhaustiveDependencies: one code per opening, see above
  useEffect(() => {
    if (address === null) {
      setStartedFor(null);
      return;
    }
    setError(null);
    setDeliveryFailed(false);
    submitted.current = "";
    form.reset();
    if (startedFor === address.id) return;
    setStartedFor(address.id);
    setCooldown(0);
    sendCode();
  }, [openedFor]);

  const close = () => {
    if (send.isPending || verify.isPending) return;
    onClose();
  };

  const resend = () => {
    if (cooldown > 0 || send.isPending) return;
    sendCode();
  };

  return (
    <Dialog open={address !== null} onOpenChange={(next) => !next && close()}>
      <DialogPopup className="sm:max-w-md">
        <form.AppForm>
          <form.Form
            label={t({
              id: "profile.emails.verify.title",
              message: "Enter the code",
            })}
            className="flex min-h-0 flex-1 flex-col"
          >
            <DialogHeader>
              <DialogTitle>
                <Trans id="profile.emails.verify.title">Enter the code</Trans>
              </DialogTitle>
            </DialogHeader>
            <DialogPanel className="flex flex-col gap-4">
              <p className="text-muted-foreground text-sm">
                <Trans id="profile.emails.verify.description">
                  A 6-digit code was sent to {address?.address}. It is valid for
                  15 minutes.
                </Trans>
              </p>
              {deliveryFailed && (
                <SurfaceAlert status="warning">
                  <SurfaceAlert.Indicator />
                  <SurfaceAlert.Content>
                    <SurfaceAlert.Title>
                      <Trans id="profile.emails.delivery_failed">
                        The email with the code could not be delivered.
                      </Trans>
                    </SurfaceAlert.Title>
                  </SurfaceAlert.Content>
                </SurfaceAlert>
              )}
              <form.AppField name="code">
                {(field) => (
                  <field.OtpField
                    label={
                      <Trans id="profile.emails.code">Verification code</Trans>
                    }
                    digits={codeLength}
                  />
                )}
              </form.AppField>
              {error !== null && (
                <p role="alert" className="text-destructive text-sm">
                  {error}
                </p>
              )}
            </DialogPanel>
            <DialogFooter>
              <Button
                variant="ghost"
                disabled={verify.isPending}
                onClick={close}
              >
                <Trans id="profile.emails.verify_later">Verify later</Trans>
              </Button>
              <div className="flex flex-col-reverse gap-2 sm:flex-row sm:justify-end">
                <Button
                  variant="ghost"
                  disabled={cooldown > 0 || send.isPending}
                  pending={send.isPending}
                  onClick={resend}
                >
                  {cooldown > 0 ? (
                    <Trans id="profile.emails.resend_cooldown">
                      Resend in {cooldown}s
                    </Trans>
                  ) : (
                    <Trans id="profile.emails.resend">Resend</Trans>
                  )}
                </Button>
                <Button
                  type="submit"
                  pending={verify.isPending}
                  disabled={code.length !== codeLength}
                >
                  <Trans id="profile.emails.verify">Verify</Trans>
                </Button>
              </div>
            </DialogFooter>
          </form.Form>
        </form.AppForm>
      </DialogPopup>
    </Dialog>
  );
}

function SetPrimaryDialog({
  address,
  onClose,
}: {
  address: EmailAddress | null;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const setPrimary = useMutation(setMyEmailPrimaryMutationOptions(queryClient));

  return (
    <ConfirmDialog
      isOpen={address !== null}
      onOpenChange={(open) => {
        if (!open && !setPrimary.isPending) onClose();
      }}
      status="warning"
      title={
        <Trans id="profile.emails.primary.title">
          Make this the primary address?
        </Trans>
      }
      subject={address?.address}
      body={
        <p>
          <Trans id="profile.emails.primary.body">
            Applications receive this address the next time you sign in. An
            application that identifies users by email address may treat you as
            a different user.
          </Trans>
        </p>
      }
      confirmLabel={
        <Trans id="profile.emails.make_primary">Make primary</Trans>
      }
      isPending={setPrimary.isPending}
      onConfirm={() => {
        if (address === null) return;
        const target = address;
        setPrimary.mutate(target.id, {
          onSuccess: () => {
            onClose();
            notifySuccess({
              ...primaryToast,
              values: { address: target.address },
            });
          },
        });
      }}
    />
  );
}

function RemoveDialog({
  address,
  onClose,
}: {
  address: EmailAddress | null;
  onClose: () => void;
}) {
  const queryClient = useQueryClient();
  const remove = useMutation(deleteMyEmailMutationOptions(queryClient));

  return (
    <ConfirmDialog
      isOpen={address !== null}
      onOpenChange={(open) => {
        if (!open && !remove.isPending) onClose();
      }}
      status="danger"
      title={<Trans id="profile.emails.delete.title">Remove address?</Trans>}
      subject={address?.address}
      body={
        <p>
          <Trans id="profile.emails.delete.body">
            To use this address again, you need to add and verify it again.
          </Trans>
        </p>
      }
      confirmLabel={<Trans id="profile.emails.delete.action">Remove</Trans>}
      isPending={remove.isPending}
      onConfirm={() => {
        if (!address) return;
        const target = address;
        remove.mutate(target.id, {
          onSuccess: () => {
            onClose();
            notifySuccess({
              ...removedToast,
              values: { address: target.address },
            });
          },
        });
      }}
    />
  );
}
