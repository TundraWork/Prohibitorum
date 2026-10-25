import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Mail, Send } from "lucide-react";
import { useEffect, useState } from "react";
import { ApiError, isCancellation } from "@/api/errors";
import { sendTestMailMutationOptions } from "@/api/mutations";
import { mailSettingsQueryOptions, myEmailsQueryOptions } from "@/api/queries";
import { quietQuery, withInlineReport } from "@/app/query-client";
import { notifySuccess } from "@/components/custom/AppNotifications";
import { Button } from "@/components/custom/Button";
import { AsyncSection, Section } from "@/components/custom/Section";
import { SettingRow } from "@/components/custom/SettingRow";
import {
  Dialog,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { applyServerError, ServerFormError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

/**
 * The section's heading, drawn by the panel and by the boundary that waits for
 * its read, so the two cannot word it differently.
 */
export const mailSection = {
  id: "mail",
  icon: <Mail />,
  title: <Trans id="settings.mail.title">Mail</Trans>,
  description: (
    <Trans id="settings.mail.description">
      Sends email verification codes. Configured in the config file; changes
      take effect after a restart.
    </Trans>
  ),
};

const sentToast = msg({
  id: "settings.mail.test.sent",
  message: "Test mail sent to {to}",
});
const rateLimitedError = msg({
  id: "settings.mail.test.rate_limited",
  message: "Too many test emails. Wait a while before sending another.",
});
const failedError = msg({
  id: "settings.mail.test.failed",
  message:
    "The mail server rejected the message or could not be reached. See the server log for the reason.",
});
const deliveryUnavailableError = msg({
  id: "settings.mail.test.delivery_unavailable",
  message: "Email sending is not enabled on this instance.",
});

/** What the config's `security` value is called on the page. */
const securityLabels = {
  starttls: "STARTTLS",
  tls: "TLS",
  none: null,
} as const;

/**
 * The instance's outbound mail: read-only, because the server and sender come
 * from the config file and only a restart can change them. The one action is
 * a test letter, offered only when mail is configured at all — an
 * unconfigured instance has nothing to test, and says instead where the
 * settings live.
 */
export function MailPanel() {
  return (
    <AsyncSection resetKey="mail" {...mailSection}>
      <MailSettings />
    </AsyncSection>
  );
}

function MailSettings() {
  const { data } = useSuspenseQuery(quietQuery(mailSettingsQueryOptions()));

  return (
    <Section
      {...mailSection}
      action={data.configured ? <TestMailButton /> : undefined}
    >
      {data.configured ? (
        <div className="flex flex-col">
          <SettingRow title={<Trans id="settings.mail.status">Status</Trans>}>
            <span className="flex items-center gap-2 text-sm">
              <span
                aria-hidden="true"
                className="inline-block size-2 rounded-full bg-success"
              />
              <Trans id="settings.mail.enabled">On</Trans>
            </span>
          </SettingRow>
          <SettingRow title={<Trans id="settings.mail.server">Server</Trans>}>
            <span className="font-mono text-[13px] text-balance">
              {data.host}:{data.port}
            </span>
          </SettingRow>
          <SettingRow
            title={<Trans id="settings.mail.security">Encryption</Trans>}
          >
            <span className="text-sm">{securityLabel(data.security)}</span>
          </SettingRow>
          <SettingRow title={<Trans id="settings.mail.from">From</Trans>}>
            <span className="font-mono text-[13px] text-balance">
              {data.from}
            </span>
          </SettingRow>
        </div>
      ) : (
        <div className="flex flex-col">
          <SettingRow title={<Trans id="settings.mail.status">Status</Trans>}>
            <span className="text-sm">
              <Trans id="settings.mail.disabled">Off</Trans>
            </span>
          </SettingRow>
          <SettingRow
            title={<Trans id="settings.mail.how_to_configure">Setup</Trans>}
            description={
              <Trans id="settings.mail.configure.hint">
                Fill in the server and sender in the smtp section of the config
                file, or set environment variables such as
                PROHIBITORUM_SMTP_HOST. See CONFIG.md.
              </Trans>
            }
          >
            <span />
          </SettingRow>
        </div>
      )}
    </Section>
  );
}

/** The label for a `security` value the server sent; unknown values show as-is. */
function securityLabel(security: string | undefined) {
  if (security === undefined) return null;
  if (security in securityLabels) return securityLabels[security as "starttls"];
  return security;
}

/**
 * Opens the test dialog. Kept as its own component so the panel above reads
 * plainly: the header holds a button when there is mail, and nothing when
 * there is not.
 */
function TestMailButton() {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button variant="outline" onClick={() => setOpen(true)}>
        <Send aria-hidden="true" />
        <Trans id="settings.mail.test">Send test mail</Trans>
      </Button>
      <TestMailDialog open={open} onClose={() => setOpen(false)} />
    </>
  );
}

/**
 * One test letter. The recipient starts as the administrator's own primary
 * address when they have one, so the usual test is one click; the failure is
 * drawn in the dialog, because the details that would explain it are in the
 * server log, not in the response.
 */
function TestMailDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const { t, i18n } = useLingui();
  // The signed-in administrator's own addresses: the default recipient. A
  // missing or failed read only leaves the box empty, so it stays quiet.
  const emails = useQuery(quietQuery(myEmailsQueryOptions()));
  const primary = emails.data?.emails?.find((email) => email.primary);
  // Every refusal the dialog can word lands in its field, so the toast stays
  // silent; the rest of the write is also shown here rather than re-thrown.
  const send = useMutation(
    withInlineReport(sendTestMailMutationOptions(), true),
  );

  const form = useAppForm({
    defaultValues: { to: "" },
    onSubmit: async ({ value }) => {
      const target = value.to.trim();
      try {
        await send.mutateAsync({ to: target, locale: i18n.locale });
        onClose();
        notifySuccess({ ...sentToast, values: { to: target } });
      } catch (failure) {
        if (isCancellation(failure)) return;
        if (failure instanceof ApiError) {
          if (failure.code === "email_invalid") {
            applyServerError(form, failure, {
              locations: {},
              codes: { email_invalid: "to" },
            });
            return;
          }
          if (failure.code === "rate_limited") {
            form.setErrorMap({
              onSubmit: {
                fields: { to: new ServerFormError(failure, rateLimitedError) },
              },
            });
            return;
          }
          if (failure.code === "email_delivery_unavailable") {
            form.setErrorMap({
              onSubmit: {
                fields: {
                  to: new ServerFormError(failure, deliveryUnavailableError),
                },
              },
            });
            return;
          }
        }
        // Anything else is still the mail subsystem's refusal; the details
        // are in the server log, not the response.
        form.setErrorMap({
          onSubmit: {
            fields: { to: new ServerFormError(failure, failedError) },
          },
        });
      }
    },
  });

  const to = useStore(form.store, (state) => state.values.to);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  // The prefill arrives with (or after) the quiet read; it only fills an
  // untouched box, never one the administrator has typed in. Keyed on the
  // address string so a refetch of the same primary does not re-run it.
  const primaryAddress = primary?.address;
  // biome-ignore lint/correctness/useExhaustiveDependencies: prefill fires once per address; `to` and the form setters are read, not triggers
  useEffect(() => {
    if (to === "" && primaryAddress !== undefined) {
      form.setFieldValue("to", primaryAddress, { dontUpdateMeta: true });
    }
  }, [primaryAddress]);

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
              id: "settings.mail.test.title",
              message: "Send test mail",
            })}
            className="flex min-h-0 flex-1 flex-col"
          >
            <DialogHeader>
              <DialogTitle>
                <Trans id="settings.mail.test.title">Send test mail</Trans>
              </DialogTitle>
            </DialogHeader>
            <DialogPanel>
              <form.AppField name="to">
                {(field) => (
                  <field.FormField
                    label={<Trans id="settings.mail.test.to">Recipient</Trans>}
                    type="email"
                    autoComplete="off"
                    autoFocus
                  />
                )}
              </form.AppField>
            </DialogPanel>
            <DialogFooter>
              <Button variant="ghost" disabled={submitting} onClick={close}>
                <Trans id="settings.mail.cancel">Cancel</Trans>
              </Button>
              <Button
                type="submit"
                pending={submitting}
                disabled={to.trim() === ""}
              >
                <Trans id="settings.mail.test.send">Send</Trans>
              </Button>
            </DialogFooter>
          </form.Form>
        </form.AppForm>
      </DialogPopup>
    </Dialog>
  );
}
