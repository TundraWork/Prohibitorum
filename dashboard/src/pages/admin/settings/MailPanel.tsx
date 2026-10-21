import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useSuspenseQuery } from "@tanstack/react-query";
import { Mail, Send } from "lucide-react";
import { useState } from "react";
import { ApiError } from "@/api/errors";
import { sendTestMailMutationOptions } from "@/api/mutations";
import { mailSettingsQueryOptions, myEmailsQueryOptions } from "@/api/queries";
import { quietQuery } from "@/app/query-client";
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
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";

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
      Used to send email verification codes. Set in the config file; takes
      effect after a restart.
    </Trans>
  ),
};

const sentToast = msg({
  id: "settings.mail.test.sent",
  message: "Test mail sent. Check {to} for it.",
});
const rateLimitedError = msg({
  id: "settings.mail.test.rate_limited",
  message: "That was too many. Try again in a moment.",
});
const failedError = msg({
  id: "settings.mail.test.failed",
  message:
    "The mail server refused this message or could not be reached. The reason is in the server log.",
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
  const { data } = useSuspenseQuery(quietQuery(mailSettingsQueryOptions()));

  return (
    <AsyncSection resetKey="mail" {...mailSection}>
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
                  Fill in the server and sender in the `smtp` section of the
                  config file, or set environment variables such as
                  PROHIBITORUM_SMTP_HOST. See CONFIG.md.
                </Trans>
              }
            >
              <span />
            </SettingRow>
          </div>
        )}
      </Section>
    </AsyncSection>
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
  const [to, setTo] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const send = useMutation(sendTestMailMutationOptions());

  const recipient = to ?? primary?.address ?? "";

  const close = () => {
    if (send.isPending) return;
    setError(null);
    setTo(null);
    onClose();
  };

  const submit = async () => {
    setError(null);
    try {
      const target = recipient.trim();
      await send.mutateAsync({ to: target, locale: i18n.locale });
      onClose();
      notifySuccess({ ...sentToast, values: { to: target } });
    } catch (failure) {
      if (failure instanceof ApiError && failure.code === "rate_limited") {
        setError(i18n._(rateLimitedError));
        return;
      }
      setError(i18n._(failedError));
    }
  };

  return (
    <Dialog open={open} onOpenChange={(next) => !next && close()}>
      <DialogPopup className="sm:max-w-md">
        <form
          className="flex min-h-0 flex-1 flex-col"
          aria-label={t({
            id: "settings.mail.test.title",
            message: "Send test mail",
          })}
          onSubmit={(event) => {
            event.preventDefault();
            if (!send.isPending && recipient.trim() !== "") void submit();
          }}
        >
          <DialogHeader>
            <DialogTitle>
              <Trans id="settings.mail.test.title">Send test mail</Trans>
            </DialogTitle>
          </DialogHeader>
          <DialogPanel>
            <Field invalid={error !== null}>
              <FieldLabel>
                <Trans id="settings.mail.test.to">Recipient</Trans>
              </FieldLabel>
              <Input
                type="email"
                autoComplete="off"
                value={recipient}
                onValueChange={(value) => {
                  setError(null);
                  setTo(value);
                }}
              />
              {error !== null && <FieldError>{error}</FieldError>}
            </Field>
          </DialogPanel>
          <DialogFooter>
            <Button variant="ghost" disabled={send.isPending} onClick={close}>
              <Trans id="settings.mail.cancel">Cancel</Trans>
            </Button>
            <Button
              type="submit"
              pending={send.isPending}
              disabled={recipient.trim() === ""}
            >
              <Trans id="settings.mail.test.send">Send</Trans>
            </Button>
          </DialogFooter>
        </form>
      </DialogPopup>
    </Dialog>
  );
}
