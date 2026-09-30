import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { isCancellation } from "@/api/errors";
import { readPrincipalSource } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateForwardAuthProjectionMutationOptions } from "@/api/mutations";
import type { PrincipalSource } from "@/api/raw-admin-paths";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { principalSourceLabel } from "@/components/custom/principal-sources";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/**
 * What the protected service receives with every request the gateway lets
 * through, laid out as the headers themselves.
 *
 * The page is about that request, so this section is where the account's
 * identity meets it: four headers, one line each, and only `Remote-User` is a
 * choice. The others are stated rather than configured — the name, the primary
 * email, the application's exposed groups.
 *
 * `Remote-User` is the identifier the service keys its users on, so a change is
 * confirmed when it is saved: the select moves freely, the save button turns to
 * its warning tone while the choice differs from the saved one, and submitting
 * opens the dialog; cancelling leaves the choice in the form, unsent. The write
 * is the projection's own endpoint, not the whole-record `PUT`, and needs no
 * step-up.
 */
export function ForwardAuthHeadersSection({ app }: { app: ForwardAuthApp }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    updateForwardAuthProjectionMutationOptions(queryClient),
  );
  // The source the dialog is confirming. It outlives the dialog's closing so
  // the text does not change while the dialog fades out.
  const [pending, setPending] = useState<PrincipalSource | null>(null);
  const [confirming, setConfirming] = useState(false);

  // The server answers with the stored source, but a record written before it
  // did reads as unknown; `username` is the server's own default, so that is
  // what the console shows rather than an empty select.
  const saved = readPrincipalSource(app.remoteUserSource) ?? "username";

  const form = useAppForm({
    defaultValues: { remoteUserSource: saved },
    onSubmit: async ({ value }) => {
      if (value.remoteUserSource === saved) return;
      setPending(value.remoteUserSource);
      setConfirming(true);
    },
  });

  async function save(remoteUserSource: PrincipalSource) {
    try {
      await update.mutateAsync({
        clientId: app.clientId,
        body: { remoteUserSource },
      });
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, { codes: {}, locations: {} });
    }
  }

  const changed = useStore(
    form.store,
    (state) => state.values.remoteUserSource !== saved,
  );

  const title = t({
    id: "admin.forward-auth-apps.headers",
    message: "Headers sent to the service",
  });
  const appName = app.displayName || app.clientId;
  const nextLabel =
    pending === null ? "" : i18n._(principalSourceLabel(pending));
  const savedLabel = i18n._(principalSourceLabel(saved));

  return (
    <Section title={title}>
      <ConsoleCard wide>
        <form.AppForm>
          <form.Form label={title}>
            <form.FormError />

            <HeaderTable>
              <HeaderRow name="Remote-User">
                <form.AppField name="remoteUserSource">
                  {(field) => (
                    <field.PrincipalSourceField
                      label="Remote-User"
                      isLabelHidden
                      className="w-full max-w-sm"
                    />
                  )}
                </form.AppField>
              </HeaderRow>
              <HeaderRow name="Remote-Name">
                <Trans id="admin.forward-auth-apps.headers.name">
                  Display name
                </Trans>
              </HeaderRow>
              <HeaderRow name="Remote-Email">
                <Trans id="admin.forward-auth-apps.headers.email">
                  Primary email
                </Trans>
              </HeaderRow>
              <HeaderRow name="Remote-Groups">
                <Trans id="admin.forward-auth-apps.headers.groups">
                  User groups this application exposes
                </Trans>
              </HeaderRow>
            </HeaderTable>

            <form.SubmitButton tone={changed ? "warning" : "default"}>
              <Trans id="admin.forward-auth-apps.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>

      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={(open) => {
          if (!open && !update.isPending) setConfirming(false);
        }}
        status="warning"
        title={
          <Trans id="admin.forward-auth-apps.headers.confirm.title">
            Change the user identifier?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.forward-auth-apps.headers.confirm.body">
              {appName} will receive the account's {nextLabel} instead of its{" "}
              {savedLabel}. Anything it saved under the old value is not
              migrated, so people it already knows will be treated as new
              accounts.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.forward-auth-apps.headers.confirm.action">
            Change and save
          </Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (pending === null) return;
          // Closed once the write settles either way: a refusal is reported
          // on the form, where the reader can act on it.
          void save(pending).finally(() => setConfirming(false));
        }}
      />
    </Section>
  );
}

/**
 * The headers as a two-column list: the header's name, then what it carries.
 *
 * It switches on its own width rather than the viewport's, since the card is
 * what has or lacks the room: wide, the names form one column and the values
 * line up beside them; narrow, each name sits over its value.
 */
function HeaderTable({ children }: { children: ReactNode }) {
  return (
    <div className="@container/headers">
      <dl className="flex flex-col divide-y divide-separator">{children}</dl>
    </div>
  );
}

/**
 * One header. The name is monospace because it is the literal string the
 * service reads, spelled as the service will see it.
 */
function HeaderRow({ name, children }: { name: string; children: ReactNode }) {
  return (
    <div className="flex flex-col gap-1.5 py-3 first:pt-0 last:pb-0 @lg/headers:grid @lg/headers:grid-cols-[9rem_minmax(0,1fr)] @lg/headers:items-center @lg/headers:gap-x-6">
      <dt className="font-mono text-sm font-medium text-foreground">{name}</dt>
      {/* A control's height on the wide table, so a row of text is as tall
          as the row holding the select and the rules fall evenly. */}
      <dd className="text-sm text-muted @lg/headers:min-h-10 @lg/headers:content-center md:@lg/headers:min-h-9">
        {children}
      </dd>
    </div>
  );
}
