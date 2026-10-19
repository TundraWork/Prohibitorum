import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { ListIcon } from "lucide-react";
import { type ReactNode, useState } from "react";
import { isCancellation } from "@/api/errors";
import { readPrincipalSource } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateForwardAuthProjectionMutationOptions } from "@/api/mutations";
import type { PrincipalSource } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { principalSourceLabel } from "@/components/custom/principal-sources";
import { Section } from "@/components/custom/Section";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/**
 * What the protected service receives with every request the gateway lets
 * through, laid out as the headers themselves.
 *
 * The page is about that request, so this section is where the account's
 * identity meets it: four headers, one row each, and only `Remote-User` is a
 * choice. The others are stated rather than configured — the name, the primary
 * email, the application's exposed groups.
 *
 * `Remote-User` is the identifier the service keys its users on, so a change is
 * confirmed when it is saved. The select moves freely, and the footer states
 * the consequence while the choice differs from the saved one, so it is
 * announced before the press; submitting then opens the dialog, and the write
 * happens from there. Cancelling leaves the choice in the form, unsent. The
 * write is the projection's own endpoint, not the whole-record `PUT`, and needs
 * no step-up.
 */
export function ForwardAuthHeadersSection({ app }: { app: ForwardAuthApp }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(
      updateForwardAuthProjectionMutationOptions(queryClient),
      true,
    ),
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
    onSubmit: ({ value }) => {
      if (value.remoteUserSource === saved) return;
      setPending(value.remoteUserSource);
      setConfirming(true);
    },
  });

  async function save(remoteUserSource: PrincipalSource) {
    try {
      const view = await update.mutateAsync({
        clientId: app.clientId,
        body: { remoteUserSource },
      });
      // The stored record is the new baseline, so the section reads as saved.
      form.reset({
        remoteUserSource:
          readPrincipalSource(view.remoteUserSource) ?? "username",
      });
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, { codes: {}, locations: {} });
    }
  }

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const title = t({
    id: "admin.forward-auth-apps.headers",
    message: "Headers sent to the service",
  });
  const appName = app.displayName || app.clientId;
  const nextLabel =
    pending === null ? "" : i18n._(principalSourceLabel(pending));
  const savedLabel = i18n._(principalSourceLabel(saved));

  return (
    <>
      <form.AppForm>
        <form.Form label={title}>
          <Section
            id="headers"
            icon={<ListIcon />}
            title={title}
            description={
              <Trans id="admin.forward-auth-apps.headers.description">
                What the service receives with every request the gateway lets
                through.
              </Trans>
            }
            dirty={dirty}
            footer={
              dirty ? (
                <ConsequenceFooter
                  consequence={
                    <Trans id="admin.forward-auth-apps.headers.consequence">
                      Changing the user identifier makes the service treat
                      everyone it already knows as a new account.
                    </Trans>
                  }
                  actionLabel={
                    <Trans id="admin.forward-auth-apps.headers.action-label">
                      Change the user identifier and save
                    </Trans>
                  }
                  onDiscard={() => form.reset()}
                  pending={submitting || update.isPending}
                />
              ) : undefined
            }
          >
            <div className="flex flex-col gap-6">
              <form.FormError />

              <Table aria-label={title}>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-40">
                      <Trans id="admin.forward-auth-apps.headers.column.header">
                        Header
                      </Trans>
                    </TableHead>
                    <TableHead>
                      <Trans id="admin.forward-auth-apps.headers.column.value">
                        Value
                      </Trans>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
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
                </TableBody>
              </Table>
            </div>
          </Section>
        </form.Form>
      </form.AppForm>

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
    </>
  );
}

/**
 * One header. The name is monospace because it is the literal string the
 * service reads, spelled as the service will see it.
 */
function HeaderRow({ name, children }: { name: string; children: ReactNode }) {
  return (
    <TableRow>
      <TableCell className="font-medium font-mono text-[13px]">
        {name}
      </TableCell>
      <TableCell className="whitespace-normal text-muted-foreground leading-normal">
        {children}
      </TableCell>
    </TableRow>
  );
}
