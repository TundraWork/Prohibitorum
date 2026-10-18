import { Label, ListBox, Select } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { useRef, useState } from "react";
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
    .filter(
      (provider) =>
        !provider.disabled && provider.ready && provider.mode !== "link_only",
    )
    .map((provider) => ({
      id: provider.slug,
      label: provider.displayName,
      description: provider.protocol,
    }));
  const providersById = new Map(
    (providers.data?.items ?? []).map((provider) => [provider.slug, provider]),
  );

  const form = useAppForm({
    defaultValues: {
      role: "user",
      username: "",
      groupIds: [] as string[],
      expectedUpstreamIdpSlug: "",
      requireLocalCredential: false,
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
      const selected = providersById.get(provider);
      const body: CreateInvitationRequest = {
        role: value.role,
        groupIds: ids,
        ...(username === "" ? {} : { username }),
        ...(provider === "" ? {} : { expectedUpstreamIdpSlug: provider }),
        requireLocalCredential:
          value.requireLocalCredential || selected?.allowSignIn === false,
      };
      try {
        const created = await create.mutateAsync(body);
        setLink(created.url);
      } catch (failure) {
        if (isCancellation(failure)) return;
        applyServerError(form, failure, {
          locations: {
            username: "username",
          },
          codes: {
            upstream_idp_link_only: "expectedUpstreamIdpSlug",
          },
        });
      }
    },
  });

  // A sign-in-off provider enforces the local-credential requirement no
  // matter what the request said: the switch is driven on and locked while one
  // is selected. Moving to a provider that does not enforce releases it again —
  // the on state was the provider's doing, not the operator's choice.
  const enforcedSlug = useStore(
    form.store,
    (state) => state.values.expectedUpstreamIdpSlug,
  );
  const enforced =
    providersById.get(enforcedSlug.trim())?.allowSignIn === false;
  const wasEnforced = useRef(false);
  if (enforced !== wasEnforced.current) {
    wasEnforced.current = enforced;
    if (enforced) {
      form.setFieldValue("requireLocalCredential", true, {
        dontUpdateMeta: true,
      });
    } else {
      form.resetField("requireLocalCredential");
    }
  }
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
                    Optional. Only providers that can create accounts are
                    offered; the invited person must sign in through the one
                    chosen here.
                  </Trans>
                }
                loading={providers.isPending}
                options={providerOptions}
                placeholder={t({
                  id: "admin.invitations.new.provider.placeholder",
                  message: "Search providers",
                })}
              />
            )}
          </form.AppField>

          <form.Subscribe
            selector={(state) =>
              [state.values.expectedUpstreamIdpSlug] as const
            }
          >
            {([slug]) => {
              const selected = providersById.get(slug.trim());
              // The value sync happens above the render; here the switch only
              // shows the enforced state, locked on.
              const enforced = selected?.allowSignIn === false;
              return (
                <form.AppField name="requireLocalCredential">
                  {(field) => (
                    <field.SwitchField
                      label={
                        <Trans id="admin.invitations.new.require_local.label">
                          Require a local sign-in method
                        </Trans>
                      }
                      description={
                        enforced ? (
                          <Trans id="admin.invitations.new.require_local.enforced">
                            This provider can't be used to sign in, so a local
                            sign-in method is always required.
                          </Trans>
                        ) : (
                          <Trans id="admin.invitations.new.require_local.hint">
                            After verifying with a provider, the invitee also
                            sets up a passkey or a password and an authenticator
                            before the account is created.
                          </Trans>
                        )
                      }
                      isDisabled={enforced}
                    />
                  )}
                </form.AppField>
              );
            }}
          </form.Subscribe>

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
