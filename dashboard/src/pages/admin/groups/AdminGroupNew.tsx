import { Description, Label, Radio, RadioGroup } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate, useRouter } from "@tanstack/react-router";
import { useState } from "react";
import { createGroupMutationOptions } from "@/api/mutations";
import type { CreateGroupRequest } from "@/api/raw-admin-paths";
import { withRouterSkipLoading } from "@/app/router";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
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
 * and the server rejects `kind` on update.
 */
export function AdminGroupNew() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const router = useRouter();
  const queryClient = useQueryClient();
  const { providers, known, state: providersState } = useRuleProviders();

  const [kind, setKind] = useState<GroupKind>("manual");
  const [tree, setTree] = useState(newRule);

  const create = useMutation(createGroupMutationOptions(queryClient));
  const ruleGroup = kind === "rule";
  const issues = checkRule(tree, known);

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
        // The pressed submit button already shows the wait while the new
        // group's page loads, so the form stays up instead of a spinner.
        await withRouterSkipLoading(router, () =>
          navigate({
            to: "/admin/groups/$groupId",
            params: { groupId: String(saved.id) },
          }),
        );
      } catch (error) {
        // `group_slug_conflict` is about one field, so it lands on that field;
        // a rule the server refused is about the document as a whole.
        applyServerError(form, error, {
          locations: {},
          codes: { group_slug_conflict: "slug" },
        });
      }
    },
  });

  return (
    <ConsoleCard
      contentClassName="flex flex-col gap-4"
      title={<Trans id="admin.group.create.title">New user group</Trans>}
    >
      <form.AppForm>
        <form.Form label={t({ id: "admin.group.form", message: "User group" })}>
          <form.FormError />

          <RadioGroup
            name="kind"
            value={kind}
            variant="secondary"
            onChange={(value) => setKind(value as GroupKind)}
          >
            <Label>
              <Trans id="admin.group.kind.label">How it fills</Trans>
            </Label>
            <Description>
              <Trans id="admin.group.kind.note">
                A rule group keeps itself up to date as accounts change. A
                manual group holds the accounts you allow or deny by hand.
              </Trans>
            </Description>
            <Radio value="manual">
              <Radio.Content>
                <Radio.Control>
                  <Radio.Indicator />
                </Radio.Control>
                <Trans id="admin.group.kind.manual">Manual</Trans>
              </Radio.Content>
            </Radio>
            <Radio value="rule">
              <Radio.Content>
                <Radio.Control>
                  <Radio.Indicator />
                </Radio.Control>
                <Trans id="admin.group.kind.rule">Rule</Trans>
              </Radio.Content>
            </Radio>
          </RadioGroup>

          <form.AppField
            name="displayName"
            validators={{ onChange: ({ value }) => validateGroupName(value) }}
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
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField
            name="slug"
            validators={{ onChange: ({ value }) => validateGroupSlug(value) }}
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
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="description">
            {(field) => (
              <field.TextAreaField
                label={<Trans id="admin.group.description">Description</Trans>}
                description={
                  <Trans id="admin.group.description.hint">
                    Optional. What this group is for, for whoever reads it next.
                  </Trans>
                }
                rows={3}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="exposedToDownstream">
            {(field) => (
              <field.SwitchField
                label={
                  <Trans id="admin.group.exposed">Send to applications</Trans>
                }
                description={
                  <Trans id="admin.group.exposed.hint">
                    Sends this group's identifier to the applications that
                    selected it, in the groups claim, the SAML groups attribute
                    and the Remote-Groups header. It does not limit which
                    applications can select the group.
                  </Trans>
                }
              />
            )}
          </form.AppField>

          {ruleGroup ? (
            <RuleBuilder
              value={tree}
              onChange={setTree}
              issues={issues}
              providers={providers}
              providersState={providersState}
              disabled={create.isPending}
            />
          ) : (
            <Description>
              <Trans id="admin.group.manual.note">
                Which accounts this group holds is decided on the group's own
                page, once it has been created.
              </Trans>
            </Description>
          )}

          <div className="flex flex-wrap gap-2">
            <Button
              disabled={create.isPending}
              onClick={() => void navigate({ to: "/admin/groups" })}
              variant="outline"
            >
              <Trans id="admin.group.cancel">Cancel</Trans>
            </Button>
            <form.SubmitButton disabled={ruleGroup && issues.length > 0}>
              <Trans id="admin.group.create">Create group</Trans>
            </form.SubmitButton>
            {ruleGroup && issues.length > 0 && (
              <p className="self-center text-destructive-foreground text-sm">
                <RuleIssueSummary issues={issues} />
              </p>
            )}
          </div>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}
