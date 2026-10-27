import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { FingerprintIcon } from "lucide-react";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { readPrincipalSource } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateOidcProjectionMutationOptions } from "@/api/mutations";
import type { UpdateOidcProjectionRequest } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
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

/** The form's values for a stored record, also the baseline after a save. */
function identityValues(app: OidcApp) {
  return {
    subjectSource: readPrincipalSource(app.subjectSource) ?? "sub",
    aliases: Object.entries(app.claimAliases ?? {}).map(
      ([name, source]): AliasRow => ({ name, source }),
    ),
  };
}

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
 * committing to one — and the footer states the consequence while the choice
 * differs from the saved one, so it is announced before the press. Submitting
 * then opens the dialog with the checked request held beside the form; the write
 * happens from the dialog, and cancelling leaves the reader's choice in the
 * form, unsent.
 *
 * The write is not sudo-gated — it changes no credential — so it submits
 * straight from the form or the dialog. A failed save is shown on the section
 * alone.
 */
export function IdentityProjectionSection({ app }: { app: OidcApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateOidcProjectionMutationOptions(queryClient), true),
  );
  // The request the dialog is confirming, checked and ready to send.
  const [pendingBody, setPendingBody] =
    useState<UpdateOidcProjectionRequest | null>(null);

  const savedSource = readPrincipalSource(app.subjectSource) ?? "sub";

  const form = useAppForm({
    defaultValues: identityValues(app),
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
      const saved = await update.mutateAsync({ clientId: app.clientId, body });
      // The stored record is the new baseline, so the section reads as saved.
      form.reset(identityValues(saved));
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, { codes: {}, locations: {} });
    }
  }

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const sourceChanged = useStore(
    form.store,
    (state) => state.values.subjectSource !== savedSource,
  );

  const title = t({
    id: "admin.oidc-apps.identity.title",
    message: "Identity projection",
  });

  return (
    <>
      <form.AppForm>
        <form.Form label={title}>
          <Section
            id="identity"
            icon={<FingerprintIcon />}
            title={title}
            description={
              <Trans id="admin.oidc-apps.identity.description">
                The user identifier and claims the client receives.
              </Trans>
            }
            dirty={dirty}
            footer={
              dirty ? (
                <ConsequenceFooter
                  consequence={
                    sourceChanged ? (
                      <Trans id="admin.oidc-apps.subject.consequence">
                        Changing the subject source makes this application treat
                        existing users as new accounts.
                      </Trans>
                    ) : undefined
                  }
                  actionLabel={
                    sourceChanged ? (
                      <Trans id="admin.oidc-apps.subject.action-label">
                        Change subject and save
                      </Trans>
                    ) : (
                      <Trans id="admin.oidc-apps.identity.save">Save</Trans>
                    )
                  }
                  onDiscard={() => form.reset()}
                  pending={submitting}
                  disabled={update.isPending}
                />
              ) : undefined
            }
          >
            <div className="flex flex-col gap-6">
              <form.FormError />

              <form.AppField name="subjectSource">
                {(field) => (
                  <field.PrincipalSourceField
                    className="w-full max-w-lg"
                    label={
                      <Trans id="admin.oidc-apps.subject">Subject source</Trans>
                    }
                    description={
                      <Trans id="admin.oidc-apps.subject.explain">
                        The identifier a client stores for each account.
                      </Trans>
                    }
                  />
                )}
              </form.AppField>

              <form.AppField name="aliases">
                {() => <AliasTableField />}
              </form.AppField>
            </div>
          </Section>
        </form.Form>
      </form.AppForm>

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
              Clients store the subject they received for each user who has
              signed in. After the source changes, clients treat those users as
              new accounts, and existing sessions no longer match them.
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
    </>
  );
}
