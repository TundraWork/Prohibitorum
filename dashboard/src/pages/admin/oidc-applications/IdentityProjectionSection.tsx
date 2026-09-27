import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { readPrincipalSource } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateOidcProjectionMutationOptions } from "@/api/mutations";
import type { UpdateOidcProjectionRequest } from "@/api/raw-admin-paths";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  AliasTableField,
  aliasRowProblem,
} from "@/pages/admin/oidc-applications/AliasTableField";
import {
  type AliasRow,
  aliasListProblem,
} from "@/pages/admin/oidc-applications/oidc-validation";

type OidcApp = components["schemas"]["OIDCApplicationView"];

/**
 * What the instance tells a client about the account it just signed in, beyond
 * the standard claims.
 *
 * Two things are decided here. The subject source picks which of the account's
 * facts becomes the `sub` a client stores — the pair (issuer, subject) is how
 * that client recognises someone on the next sign-in, so changing it makes every
 * client treat the account as a stranger. And the aliases copy one granted claim
 * to a second name, for a client that expects a claim the specification does not
 * define; they are a two-column table because each is a pair of claim names.
 *
 * The subject source is confirmed when the form is saved, not when it is
 * picked. The select is free to move — a reader comparing the options is not
 * committing to one — and the save button turns to its warning tone while the
 * choice differs from the saved one, so the consequence is announced before the
 * press. Submitting then opens the dialog with the checked request held beside
 * the form; the write happens from the dialog, and cancelling leaves the
 * reader's choice in the form, unsent.
 *
 * The write is not sudo-gated — it changes no credential — so it submits
 * straight from the form or the dialog.
 */
export function IdentityProjectionSection({ app }: { app: OidcApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateOidcProjectionMutationOptions(queryClient));
  // The request the dialog is confirming, checked and ready to send.
  const [pendingBody, setPendingBody] =
    useState<UpdateOidcProjectionRequest | null>(null);

  const savedSource = readPrincipalSource(app.subjectSource) ?? "sub";

  const form = useAppForm({
    defaultValues: {
      subjectSource: savedSource,
      aliases: Object.entries(app.claimAliases ?? {}).map(
        ([name, source]): AliasRow => ({ name, source }),
      ),
    },
    onSubmit: async ({ value }) => {
      const problem = aliasListProblem(value.aliases);
      if (problem !== undefined) {
        form.setFieldMeta("aliases", (meta) => ({
          ...meta,
          errorMap: { ...meta.errorMap, onSubmit: [aliasRowProblem(problem)] },
        }));
        return;
      }

      const body = {
        subjectSource: value.subjectSource,
        claimAliases: Object.fromEntries(
          value.aliases.map((row) => [row.name, row.source]),
        ),
      };
      if (value.subjectSource !== savedSource) {
        setPendingBody(body);
        return;
      }
      await save(body);
    },
  });

  async function save(body: UpdateOidcProjectionRequest) {
    try {
      await update.mutateAsync({ clientId: app.clientId, body });
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, { codes: {}, locations: {} });
    }
  }

  const sourceChanged = useStore(
    form.store,
    (state) => state.values.subjectSource !== savedSource,
  );

  return (
    <Section
      title={<Trans id="admin.oidc-apps.identity">Identity projection</Trans>}
    >
      <ConsoleCard>
        <form.AppForm>
          <form.Form
            label={t({
              id: "admin.oidc-apps.identity",
              message: "Identity projection",
            })}
          >
            <form.FormError />

            <form.AppField name="subjectSource">
              {(field) => (
                <field.PrincipalSourceField
                  label={
                    <Trans id="admin.oidc-apps.subject">Subject source</Trans>
                  }
                  description={
                    <Trans id="admin.oidc-apps.subject.hint">
                      The identifier a client stores for this account. Changing
                      it signs everyone out of every client.
                    </Trans>
                  }
                />
              )}
            </form.AppField>

            <form.AppField name="aliases">
              {() => <AliasTableField />}
            </form.AppField>

            <form.SubmitButton tone={sourceChanged ? "warning" : "default"}>
              <Trans id="admin.oidc-apps.identity.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>

      <ConfirmDialog
        isOpen={pendingBody !== null}
        onOpenChange={(open) => {
          if (!open && !update.isPending) setPendingBody(null);
        }}
        status="warning"
        title={
          <Trans id="admin.oidc-apps.subject.confirm.title">
            Change the subject source?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.oidc-apps.subject.confirm.body">
              Every client that has signed this account in before stores the
              subject it was given. Changing the source makes each of them see a
              new account, so existing sessions at those clients no longer
              recognise it.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.oidc-apps.subject.confirm.save">
            Change and save
          </Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (pendingBody === null) return;
          // Closed once the write settles either way: a refusal is reported
          // on the form, where the reader can act on it.
          void save(pendingBody).finally(() => setPendingBody(null));
        }}
      />
    </Section>
  );
}
