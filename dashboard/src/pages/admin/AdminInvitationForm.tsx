import { Label, ListBox, Select } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { createInvitationMutationOptions } from "@/api/mutations";
import {
  groupsQueryOptions,
  identityProvidersQueryOptions,
} from "@/api/queries";
import type {
  AppGroupView,
  CreateInvitationRequest,
} from "@/api/raw-admin-paths";
import { Button } from "@/components/custom/Button";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import type { EntityOption } from "@/components/custom/EntityPicker";
import { SecretReveal } from "@/components/custom/SecretReveal";
import { invitationLinkCopy } from "@/components/custom/secret-reveal-copy";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

const roleUser = msg({ id: "admin.invitations.role.user", message: "User" });
const roleAdmin = msg({ id: "admin.invitations.role.admin", message: "Admin" });

/**
 * A group id the form cannot send: the server takes integers, not the strings a
 * picker hands back, so a non-integer never leaves the browser.
 */
const groupIdInvalid = msg({
  id: "admin.invitations.new.groups.invalid",
  message: "Choose the groups again — one of them is not a valid group.",
});

/**
 * Creates one invitation.
 *
 * An invitation is the only way an account comes into being, so this form
 * decides everything about the account up front: its role, the manual groups it
 * joins, and optionally the upstream provider and username it is bound to.
 *
 * Two server behaviours shape the body. An empty string is not the same request
 * as an omitted field — the server validates `username` and rejects a slug it
 * does not know — so fields the admin left blank are dropped rather than sent
 * empty. And a provider that is disabled or not ready cannot sign anyone in:
 * an invitation bound to one would be un-redeemable, so the picker offers only
 * providers that can actually authenticate.
 */
export function AdminInvitationForm() {
  const { t } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const groups = useQuery(groupsQueryOptions());
  const providers = useQuery(identityProvidersQueryOptions());
  const create = useMutation(createInvitationMutationOptions(queryClient));
  const [link, setLink] = useState<string | null>(null);

  const groupOptions: EntityOption[] = (groups.data ?? [])
    // A rule group's membership is decided by its rule, so an invitation can
    // only carry manual ones; the server rejects the others.
    .filter((group: AppGroupView) => group.kind === "manual")
    .map((group: AppGroupView) => ({
      id: String(group.id),
      label: group.displayName,
      description: group.slug,
    }));

  const providerOptions: EntityOption[] = (providers.data?.items ?? [])
    .filter((provider) => !provider.disabled && provider.ready)
    .map((provider) => ({
      id: provider.slug,
      label: provider.displayName,
      description: provider.protocol,
    }));

  const form = useAppForm({
    defaultValues: {
      role: "user",
      username: "",
      groupIds: [] as string[],
      expectedUpstreamIdpSlug: "",
    },
    onSubmit: async ({ value }) => {
      const username = value.username.trim();
      const provider = value.expectedUpstreamIdpSlug.trim();
      const ids = value.groupIds.map(Number);
      if (ids.some((id) => !Number.isInteger(id))) {
        form.setErrorMap({
          onSubmit: { form: groupIdInvalid, fields: {} },
        });
        return;
      }
      const body: CreateInvitationRequest = {
        role: value.role,
        groupIds: ids,
        ...(username === "" ? {} : { username }),
        ...(provider === "" ? {} : { expectedUpstreamIdpSlug: provider }),
      };
      try {
        const created = await create.mutateAsync(body);
        setLink(created.url);
      } catch (failure) {
        if (isCancellation(failure)) return;
        applyServerError(form, failure, {
          locations: {
            username: "username",
            expected_upstream_idp_slug: "expectedUpstreamIdpSlug",
          },
          codes: {},
        });
      }
    },
  });

  if (link !== null) {
    return (
      <SecretReveal
        text={link}
        filename="prohibitorum-invitation.txt"
        copy={invitationLinkCopy}
        onContinue={async () => {
          await navigate({ to: "/admin/invitations" });
        }}
      />
    );
  }

  return (
    <ConsoleCard
      title={<Trans id="admin.invitations.new.title">New invitation</Trans>}
    >
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.invitations.new.form",
            message: "New invitation",
          })}
        >
          <form.FormError />

          <form.AppField name="role">
            {(field) => (
              <Select
                className="w-full"
                variant="secondary"
                value={field.state.value}
                onChange={(key) => {
                  if (typeof key === "string") field.handleChange(key);
                }}
              >
                <Label>
                  <Trans id="admin.invitations.new.role">Role</Trans>
                </Label>
                <Select.Trigger>
                  <Select.Value />
                  <Select.Indicator />
                </Select.Trigger>
                <Select.Popover>
                  <ListBox>
                    <ListBox.Item id="user" textValue={t(roleUser)}>
                      {t(roleUser)}
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                    <ListBox.Item id="admin" textValue={t(roleAdmin)}>
                      {t(roleAdmin)}
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                  </ListBox>
                </Select.Popover>
              </Select>
            )}
          </form.AppField>

          <form.AppField name="username">
            {(field) => (
              <field.FormField
                label={
                  <Trans id="admin.invitations.new.username">
                    Account username
                  </Trans>
                }
                description={
                  <Trans id="admin.invitations.new.username.hint">
                    Optional. Leave it empty to let the invited person choose
                    their own when they register.
                  </Trans>
                }
                autoComplete="off"
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="groupIds">
            {(field) => (
              <field.GroupPicker
                description={
                  <Trans id="admin.invitations.new.groups.hint">
                    The manual groups the new account joins when it registers.
                  </Trans>
                }
                loading={groups.isPending}
                options={groupOptions}
                variant="secondary"
                placeholder={t({
                  id: "admin.invitations.new.groups.placeholder",
                  message: "Search user groups",
                })}
              />
            )}
          </form.AppField>

          <form.AppField name="expectedUpstreamIdpSlug">
            {(field) => (
              <field.IdentityProviderPicker
                description={
                  <Trans id="admin.invitations.new.provider.hint">
                    Optional. Only providers that are enabled and ready are
                    offered; the invited person must sign in through the one
                    chosen here.
                  </Trans>
                }
                loading={providers.isPending}
                options={providerOptions}
                variant="secondary"
                placeholder={t({
                  id: "admin.invitations.new.provider.placeholder",
                  message: "Search providers",
                })}
              />
            )}
          </form.AppField>

          <div className="flex flex-wrap gap-2">
            <Button
              variant="secondary"
              onPress={() => void navigate({ to: "/admin/invitations" })}
            >
              <Trans id="admin.cancel">Cancel</Trans>
            </Button>
            <form.SubmitButton>
              <Trans id="admin.invitations.new.submit">Create invitation</Trans>
            </form.SubmitButton>
          </div>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}
