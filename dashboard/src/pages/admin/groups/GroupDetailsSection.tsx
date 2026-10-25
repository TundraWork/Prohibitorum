import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { InfoIcon } from "lucide-react";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { updateGroupMutationOptions } from "@/api/mutations";
import type { AppGroupView, UpdateGroupRequest } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  groupUpdateBody,
  validateGroupName,
  validateGroupSlug,
} from "@/pages/admin/groups/group-validation";

/** The form's values for a stored record, also the baseline after a save. */
function detailsValues(group: AppGroupView) {
  return {
    displayName: group.displayName,
    slug: group.slug,
    description: group.description ?? "",
    exposedToDownstream: group.exposedToDownstream,
  };
}

/**
 * The group's name, identifier and description, and whether its identifier is
 * sent to the applications that selected it. One save covers all four.
 *
 * Turning sending off is confirmed when the form is saved, not when the switch
 * moves: the footer states what applications stop receiving and names the
 * action while the switch differs from the saved one, and submitting then opens
 * a short dialog with the checked request held beside the form. The write
 * happens from the dialog, and cancelling leaves the choice in the form, unsent.
 *
 * The kind is shown, not edited: the server rejects it on update. The request
 * carries no rule, which the server reads as "keep the current one".
 */
export function GroupDetailsSection({ group }: { group: AppGroupView }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  // Named, so the messages carry them as `{name}` and `{slug}` in both catalogs.
  const name = group.displayName;
  const slug = group.slug;
  const update = useMutation(
    withInlineReport(updateGroupMutationOptions(queryClient), true),
  );
  // The request the dialog is confirming, checked and ready to send.
  const [pendingBody, setPendingBody] = useState<UpdateGroupRequest | null>(
    null,
  );

  const form = useAppForm({
    defaultValues: detailsValues(group),
    onSubmit: async ({ value }) => {
      const body = groupUpdateBody(group, {
        slug: value.slug.trim(),
        displayName: value.displayName.trim(),
        description: value.description.trim(),
        exposedToDownstream: value.exposedToDownstream,
      });
      if (group.exposedToDownstream && !value.exposedToDownstream) {
        setPendingBody(body);
        return;
      }
      await save(body);
    },
  });

  async function save(body: UpdateGroupRequest) {
    try {
      const saved = await update.mutateAsync({ groupId: group.id, body });
      // The stored record is the new baseline, so the section reads as saved.
      form.reset(detailsValues(saved));
    } catch (error) {
      if (isCancellation(error)) return;
      // A taken identifier is about one field; anything else is the form's.
      applyServerError(form, error, {
        locations: {},
        codes: { group_slug_conflict: "slug" },
      });
    }
  }

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const turningOff = useStore(
    form.store,
    (state) => group.exposedToDownstream && !state.values.exposedToDownstream,
  );

  const title = t({ id: "admin.group.details.title", message: "Details" });

  return (
    <>
      <form.AppForm>
        <form.Form label={title}>
          <Section
            id="details"
            icon={<InfoIcon />}
            title={title}
            description={
              <Trans id="admin.group.details.description">
                Set the group's name and identifier, and whether the identifier
                is sent to applications.
              </Trans>
            }
            dirty={dirty}
            footer={
              dirty ? (
                <ConsequenceFooter
                  consequence={
                    turningOff ? (
                      <Trans id="admin.group.exposed.consequence">
                        Applications that selected {name} stop receiving {slug}{" "}
                        at the next sign-in.
                      </Trans>
                    ) : undefined
                  }
                  actionLabel={
                    turningOff ? (
                      <Trans id="admin.group.exposed.action-label">
                        Stop sending and save
                      </Trans>
                    ) : (
                      <Trans id="admin.group.details.save">Save</Trans>
                    )
                  }
                  onDiscard={() => form.reset()}
                  pending={submitting || update.isPending}
                />
              ) : undefined
            }
          >
            <div className="flex flex-col gap-6">
              <form.FormError />

              <form.AppField
                name="displayName"
                validators={{
                  onChange: ({ value }) => validateGroupName(value),
                }}
              >
                {(field) => (
                  <field.FormField
                    label={<Trans id="admin.group.display-name">Name</Trans>}
                    description={
                      <Trans id="admin.group.display-name.hint">
                        The name shown in the console.
                      </Trans>
                    }
                    autoComplete="off"
                  />
                )}
              </form.AppField>

              <form.AppField
                name="slug"
                validators={{
                  onChange: ({ value }) => validateGroupSlug(value),
                }}
              >
                {(field) => (
                  <field.FormField
                    label={<Trans id="admin.group.slug">Identifier</Trans>}
                    description={
                      <Trans id="admin.group.slug.hint">
                        Lowercase letters, digits and hyphens. Applications
                        refer to the group by this identifier.
                      </Trans>
                    }
                    autoComplete="off"
                    autoCapitalize="none"
                    spellCheck={false}
                    isMonospace
                    compactValue
                  />
                )}
              </form.AppField>

              <form.AppField name="description">
                {(field) => (
                  <field.TextAreaField
                    label={
                      <Trans id="admin.group.description">Description</Trans>
                    }
                    description={
                      <Trans id="admin.group.description.hint">
                        Optional. Describe what this group is for.
                      </Trans>
                    }
                    rows={3}
                  />
                )}
              </form.AppField>

              <form.AppField name="exposedToDownstream">
                {(field) => (
                  <field.SwitchField
                    label={
                      <Trans id="admin.group.exposed">
                        Send to applications
                      </Trans>
                    }
                    description={
                      <Trans id="admin.group.exposed.hint">
                        Sends this group's identifier to the applications that
                        selected it, in the groups claim, the SAML groups
                        attribute and the Remote-Groups header. It does not
                        limit which applications can select the group.
                      </Trans>
                    }
                  />
                )}
              </form.AppField>

              <ReadOnlyKind kind={group.kind} />
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
          <Trans id="admin.group.exposed.confirm.title">
            Stop sending {name} to applications?
          </Trans>
        }
        body={
          <p>
            <Trans id="admin.group.exposed.confirm.body">
              Applications stop receiving the group's identifier. The group can
              still be selected for access, and this can be turned back on at
              any time.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="admin.group.exposed.confirm.action">Stop sending</Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (pendingBody === null) return;
          // Closed once the write settles either way: a refusal is reported on
          // the form, where the reader can act on it.
          void save(pendingBody).finally(() => setPendingBody(null));
        }}
      />
    </>
  );
}

/**
 * The kind as a fact: it is set when the group is created and never changes,
 * so it reads as text, not as a field that takes a caret.
 */
function ReadOnlyKind({ kind }: { kind: string }) {
  const { t } = useLingui();
  return (
    <dl className="flex flex-col items-start gap-2">
      <dt className="font-medium text-base/4.5 sm:text-sm/4">
        <Trans id="admin.group.kind.label">Membership</Trans>
      </dt>
      <dd className="text-base sm:text-sm">
        {kind === "rule"
          ? t({ id: "admin.group.kind.rule", message: "Rule" })
          : t({ id: "admin.group.kind.manual", message: "Manual" })}
      </dd>
      <dd className="text-muted-foreground text-xs">
        <Trans id="admin.group.kind.fixed">
          A group's kind is set when it is created and cannot be changed.
        </Trans>
      </dd>
    </dl>
  );
}
