import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { InfoIcon, ListFilterIcon, ShapesIcon } from "lucide-react";
import { useId, useState } from "react";
import { isCancellation } from "@/api/errors";
import { createGroupMutationOptions } from "@/api/mutations";
import type { CreateGroupRequest } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { Section } from "@/components/custom/Section";
import { RadioGroup } from "@/components/ui/radio-group";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  validateGroupName,
  validateGroupSlug,
} from "@/pages/admin/groups/group-validation";
import { RuleBuilder } from "@/pages/admin/groups/RuleBuilder";
import { RuleIssueSummary } from "@/pages/admin/groups/RuleIssueSummary";
import {
  checkRule,
  newRule,
  serializeRule,
} from "@/pages/admin/groups/rule-model";
import { useRuleProviders } from "@/pages/admin/groups/use-rule-providers";

type GroupKind = "manual" | "rule";

/**
 * Creates a user group.
 *
 * The kind is chosen once, here: a manual group collects its decisions one
 * account at a time on its own page, a rule group is described by a document,
 * and the server rejects `kind` on update. Only what the server cannot default
 * is asked for; who a manual group allows or denies is decided on its page.
 *
 * A rule the builder already knows is invalid holds Create back, with the
 * reason beside the button. There is no count of matching accounts yet: the
 * preview runs against a saved group's page.
 *
 * A failed create is shown on the form or the field it belongs to, and the
 * mutation marks itself inline so the failure is not also a toast.
 */
export function AdminGroupNew() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const { providers, known, state: providersState } = useRuleProviders();
  const create = useMutation(
    withInlineReport(createGroupMutationOptions(queryClient), true),
  );

  const [kind, setKind] = useState<GroupKind>("manual");
  const [tree, setTree] = useState(newRule);
  const reasonId = useId();

  const ruleGroup = kind === "rule";
  const issues = checkRule(tree, known);
  const blocked = ruleGroup && issues.length > 0;

  const form = useAppForm({
    defaultValues: {
      displayName: "",
      slug: "",
      description: "",
      exposedToDownstream: true,
    },
    onSubmit: async ({ value }) => {
      const request: CreateGroupRequest = {
        slug: value.slug.trim(),
        displayName: value.displayName.trim(),
        description: value.description.trim(),
        exposedToDownstream: value.exposedToDownstream,
        kind,
      };
      // A rule on a manual group is rejected outright, so it is left off the
      // request rather than sent empty.
      if (ruleGroup) request.rule = serializeRule(tree);
      try {
        const saved = await create.mutateAsync(request);
        await navigate({
          to: "/admin/groups/$groupId",
          params: { groupId: String(saved.id) },
        });
      } catch (error) {
        if (isCancellation(error)) return;
        // `group_slug_conflict` is about one field; a rule the server refused
        // is about the document as a whole, so it stays on the form.
        applyServerError(form, error, {
          locations: {},
          codes: { group_slug_conflict: "slug" },
        });
      }
    },
  });
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  return (
    <PageBody>
      <PageHeader
        title={<Trans id="admin.group.create.title">New user group</Trans>}
        description={
          <Trans id="admin.group.create.description">
            Create a group applications can use to decide who gets in.
          </Trans>
        }
      />
      <form.AppForm>
        <form.Form
          label={t({ id: "admin.group.form", message: "User group" })}
          className="flex flex-col gap-8"
        >
          <form.FormError />

          <Section
            id="kind"
            icon={<ShapesIcon />}
            title={<Trans id="admin.group.create.kind.title">Kind</Trans>}
            description={
              <Trans id="admin.group.kind.fixed">
                A group's kind is set when it is created and cannot be changed.
              </Trans>
            }
          >
            <RadioGroup
              aria-label={t({
                id: "admin.group.create.kind.title",
                message: "Kind",
              })}
              value={kind}
              disabled={submitting}
              onValueChange={(next) => setKind(next as GroupKind)}
            >
              <ChoiceTile
                layout="list"
                value="manual"
                label={<Trans id="admin.group.kind.manual">Manual</Trans>}
                description={
                  <Trans id="admin.group.create.kind.manual.description">
                    A manual group holds the accounts you allow or deny by hand.
                  </Trans>
                }
              />
              <ChoiceTile
                layout="list"
                value="rule"
                label={<Trans id="admin.group.kind.rule">Rule</Trans>}
                description={
                  <Trans id="admin.group.create.kind.rule.description">
                    A rule group keeps itself up to date as accounts change.
                  </Trans>
                }
              />
            </RadioGroup>
          </Section>

          <Section
            id="details"
            icon={<InfoIcon />}
            title={<Trans id="admin.group.details.title">Details</Trans>}
            description={
              <Trans id="admin.group.details.description">
                What the group is called, how applications name it, and whether
                they are sent that name.
              </Trans>
            }
          >
            <div className="flex flex-col gap-6">
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
                        What this group is called in the console.
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
                        Lowercase letters, digits and hyphens. Applications name
                        this group by it.
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
                        Optional. What this group is for, for whoever reads it
                        next.
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
            </div>
          </Section>

          {ruleGroup ? (
            <Section
              id="rule"
              icon={<ListFilterIcon />}
              title={
                <Trans id="admin.group.rule.heading">
                  Who this group matches
                </Trans>
              }
              description={
                <Trans id="admin.group.rule.description">
                  Checked every time someone asks for access, so the group is
                  always current.
                </Trans>
              }
            >
              <div className="flex flex-col gap-4">
                <RuleBuilder
                  value={tree}
                  onChange={setTree}
                  issues={issues}
                  providers={providers}
                  providersState={providersState}
                  disabled={submitting}
                />
                <p className="text-muted-foreground text-sm">
                  <Trans id="admin.group.create.rule.preview">
                    Matching accounts appear once the group is saved.
                  </Trans>
                </p>
              </div>
            </Section>
          ) : (
            <p className="text-muted-foreground text-sm">
              <Trans id="admin.group.create.manual.note">
                You choose who this group allows or denies on its page after you
                create it.
              </Trans>
            </p>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <form.SubmitButton
              disabled={blocked}
              describedBy={blocked ? reasonId : undefined}
            >
              <Trans id="admin.group.create">Create group</Trans>
            </form.SubmitButton>
            <Button variant="ghost" render={<Link to="/admin/groups" />}>
              <Trans id="admin.cancel">Cancel</Trans>
            </Button>
            {blocked && (
              <p id={reasonId} className="text-destructive-foreground text-sm">
                <RuleIssueSummary issues={issues} />
              </p>
            )}
          </div>
        </form.Form>
      </form.AppForm>
    </PageBody>
  );
}
