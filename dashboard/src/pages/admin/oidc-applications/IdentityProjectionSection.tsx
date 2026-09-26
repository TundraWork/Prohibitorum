import { Label, ListBox, Select } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { readPrincipalSource, reservedAliasNames } from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateOidcProjectionMutationOptions } from "@/api/mutations";
import type { PrincipalSource } from "@/api/raw-admin-paths";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { RowsField, rowProblem } from "@/components/custom/RowsField";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  type AliasRow,
  AliasRowFields,
} from "@/pages/admin/oidc-applications/AliasRowFields";

type OidcApp = components["schemas"]["OIDCApplicationView"];

/**
 * An alias's output name, as the server validates it: an identifier-shaped
 * claim name that no ID token already defines.
 */
const aliasNamePattern = /^[A-Za-z_][A-Za-z0-9_]{0,63}$/;

const aliasNameInvalid = msg({
  id: "admin.oidc-apps.alias.name.invalid",
  message:
    "Start with a letter or an underscore, then letters, digits or underscores, up to 64 characters.",
});

const aliasNameReserved = msg({
  id: "admin.oidc-apps.alias.name.reserved",
  message: "The ID token already defines this claim.",
});

const aliasNameDuplicate = msg({
  id: "admin.oidc-apps.alias.name.duplicate",
  message: "Another alias already uses this name.",
});

const aliasNameRequired = msg({
  id: "admin.oidc-apps.alias.name.required",
  message: "Enter the claim name the client will read.",
});

/**
 * What the instance tells a client about the account it just signed in, beyond
 * the standard claims.
 *
 * Two things are decided here. The subject source picks which of the account's
 * facts becomes the `sub` a client stores — the pair (issuer, subject) is how
 * that client recognises someone on the next sign-in, so changing it makes every
 * client treat the account as a stranger. And the aliases copy one granted claim
 * to a second name, for a client that expects a claim the specification does not
 * define; they are written as rows because each is a small pair rather than a
 * sentence.
 *
 * The write is not sudo-gated — it changes no credential — so it submits
 * straight from the form.
 */
export function IdentityProjectionSection({ app }: { app: OidcApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateOidcProjectionMutationOptions(queryClient));
  const [confirmingSource, setConfirmingSource] = useState(false);
  // Held beside the form because the confirmation has to be able to cancel the
  // change: a dialog that reads the form's own value could only confirm it.
  const [pendingSource, setPendingSource] = useState<PrincipalSource | null>(
    null,
  );

  const form = useAppForm({
    defaultValues: {
      subjectSource: readPrincipalSource(app.subjectSource) ?? "sub",
      aliases: Object.entries(app.claimAliases ?? {}).map(
        ([name, source]): AliasRow => ({ name, source }),
      ),
    },
    onSubmit: async ({ value }) => {
      // The rows are checked in one pass and the first bad one is reported: an
      // alias has no identity until it is saved, so the message can only name
      // the row by its position, and several messages at once would each point
      // at a box the reader has to find.
      const aliases: Record<string, string> = {};
      const complain = (index: number, reason: typeof aliasNameInvalid) => {
        form.setFieldMeta("aliases", (meta) => ({
          ...meta,
          errors: [rowProblem(index, { line: index + 1, reason })],
        }));
      };
      for (const [index, row] of value.aliases.entries()) {
        const name = row.name.trim();
        if (name === "") {
          complain(index, aliasNameRequired);
          return;
        }
        if (name.length > 64 || !aliasNamePattern.test(name)) {
          complain(index, aliasNameInvalid);
          return;
        }
        if (reservedAliasNames.includes(name)) {
          complain(index, aliasNameReserved);
          return;
        }
        if (Object.hasOwn(aliases, name)) {
          complain(index, aliasNameDuplicate);
          return;
        }
        aliases[name] = row.source;
      }

      try {
        await update.mutateAsync({
          clientId: app.clientId,
          body: {
            subjectSource: value.subjectSource,
            claimAliases: aliases,
          },
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, { codes: {}, locations: {} });
      }
    },
  });

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
                <div className="flex flex-col gap-1.5">
                  <Select
                    className="w-full"
                    variant="secondary"
                    value={field.state.value}
                    onChange={(key) => {
                      if (typeof key !== "string") return;
                      if (key === field.state.value) return;
                      // Changing this makes every client see a new subject, so
                      // it is confirmed before it is stored — but the field is
                      // not written until the dialog agrees, or the form would
                      // already hold the new source if the reader cancels.
                      setPendingSource(key as PrincipalSource);
                      setConfirmingSource(true);
                    }}
                  >
                    <Label>
                      <Trans id="admin.oidc-apps.subject">Subject source</Trans>
                    </Label>
                    <Select.Trigger>
                      <Select.Value />
                      <Select.Indicator />
                    </Select.Trigger>
                    <Select.Popover>
                      <ListBox>
                        <ListBox.Item id="sub" textValue="sub">
                          <span className="font-mono text-sm">sub</span>
                          <ListBox.ItemIndicator />
                        </ListBox.Item>
                        <ListBox.Item
                          id="username"
                          textValue={t({
                            id: "admin.oidc-apps.subject.username",
                            message: "Username",
                          })}
                        >
                          <Trans id="admin.oidc-apps.subject.username">
                            Username
                          </Trans>
                          <ListBox.ItemIndicator />
                        </ListBox.Item>
                        <ListBox.Item
                          id="verified_email"
                          textValue={t({
                            id: "admin.oidc-apps.subject.email",
                            message: "Verified e-mail address",
                          })}
                        >
                          <Trans id="admin.oidc-apps.subject.email">
                            Verified e-mail address
                          </Trans>
                          <ListBox.ItemIndicator />
                        </ListBox.Item>
                      </ListBox>
                    </Select.Popover>
                  </Select>
                  <p className="text-xs text-muted">
                    <Trans id="admin.oidc-apps.subject.hint">
                      The identifier a client stores for this account. Changing
                      it signs everyone out of every client.
                    </Trans>
                  </p>
                </div>
              )}
            </form.AppField>

            <form.AppField name="aliases">
              {() => (
                <RowsField<AliasRow>
                  label={
                    <Trans id="admin.oidc-apps.aliases">Claim aliases</Trans>
                  }
                  description={
                    <Trans id="admin.oidc-apps.aliases.hint">
                      Publish a granted claim under a second name, for a client
                      that expects one the specification does not define.
                    </Trans>
                  }
                  emptyRow={() => ({ name: "", source: "name" })}
                  addLabel={
                    <Trans id="admin.oidc-apps.aliases.add">Add alias</Trans>
                  }
                  renderRow={(_row, index, error) => (
                    <AliasRowFields index={index} error={error} />
                  )}
                />
              )}
            </form.AppField>

            <form.SubmitButton>
              <Trans id="admin.oidc-apps.identity.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>

      <ConfirmDialog
        isOpen={confirmingSource}
        onOpenChange={(open) => {
          if (!open) {
            setConfirmingSource(false);
            setPendingSource(null);
          }
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
          <Trans id="admin.oidc-apps.subject.confirm.action">Change</Trans>
        }
        onConfirm={() => {
          if (pendingSource !== null) {
            form.setFieldValue("subjectSource", pendingSource);
          }
          setConfirmingSource(false);
          setPendingSource(null);
        }}
      />
    </Section>
  );
}
