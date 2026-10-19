import { Trans, useLingui } from "@lingui/react/macro";
import type { UseMutationResult } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import { Button } from "@/components/custom/Button";
import {
  NewPasswordFields,
  newPasswordFormValues,
} from "@/components/custom/NewPasswordFields";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type ChangePassword = UseMutationResult<void, Error, string>;

/**
 * Changing the password is one step, so it is one dialog. The write belongs to
 * the panel, which announces the change once the request succeeds; the dialog
 * only closes. While it is in flight the dialog stays up, so a refusal lands on
 * the field it belongs to.
 */
export function ChangePasswordDialog({
  open,
  change,
  onClose,
}: {
  open: boolean;
  change: ChangePassword;
  onClose: () => void;
}) {
  return (
    <Dialog
      open={open}
      onOpenChange={(next) => !next && !change.isPending && onClose()}
    >
      <DialogPopup className="sm:max-w-md">
        <ChangePasswordForm change={change} onClose={onClose} />
      </DialogPopup>
    </Dialog>
  );
}

function ChangePasswordForm({
  change,
  onClose,
}: {
  change: ChangePassword;
  onClose: () => void;
}) {
  const { t } = useLingui();
  const form = useAppForm({
    defaultValues: newPasswordFormValues,
    onSubmit: async ({ value }) => {
      try {
        await change.mutateAsync(value.password);
        onClose();
      } catch (error) {
        // Backing out of the identity check is a decision, not a failure.
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          locations: { password: "password" },
          codes: {},
        });
      }
    },
  });

  return (
    <form.AppForm>
      <DialogHeader>
        <DialogTitle>
          <Trans id="security.password.change.title">Change password</Trans>
        </DialogTitle>
      </DialogHeader>
      <form.Form
        label={t({
          id: "security.password.change.form",
          message: "Change password",
        })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <DialogPanel className="flex flex-col gap-4">
          <form.FormError />
          <NewPasswordFields
            form={form}
            passwordLabel={
              <Trans id="security.password.new">New password</Trans>
            }
            confirmLabel={
              <Trans id="security.password.confirm">Repeat new password</Trans>
            }
          />
        </DialogPanel>
        <DialogFooter>
          <Button variant="ghost" disabled={change.isPending} onClick={onClose}>
            <Trans id="security.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton fullWidth>
            <Trans id="security.password.change.action">Change password</Trans>
          </form.SubmitButton>
        </DialogFooter>
      </form.Form>
    </form.AppForm>
  );
}
