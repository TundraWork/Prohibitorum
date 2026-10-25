import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { LogInIcon, ShieldIcon, UserRoundIcon } from "lucide-react";
import { useEffect, useRef, useState } from "react";
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
import { quietQuery, withInlineReport } from "@/app/query-client";
import { CancelLink } from "@/components/custom/CancelLink";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { useConsoleCrumb } from "@/components/custom/console-crumb";
import type { EntityOption } from "@/components/custom/EntityPicker";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { SecretRevealDialog } from "@/components/custom/SecretReveal";
import { Section, SectionFailure } from "@/components/custom/Section";
import { invitationLinkCopyWithExpiry } from "@/components/custom/secret-reveal-copy";
import { RadioGroup } from "@/components/ui/radio-group";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { providerProtocolLabel } from "@/pages/admin/identity-providers/provider-options";

/**
 * A group id the form cannot send: the server takes integers, not the strings a
 * picker hands back, so a non-integer never leaves the browser.
 */
const groupIdInvalid = msg({
  id: "admin.invitations.new.groups.invalid",
  message: "One of the selected groups is not valid. Choose the groups again.",
});

/**
 * Creates one invitation.
 *
 * An empty string is not the same request as an omitted field (the server
 * validates `username` and rejects a slug it does not know), so blank fields
 * are dropped. A provider that is disabled, not ready or link-only cannot
 * redeem an invitation, so the picker does not offer it.
 *
 * The link comes back once, so it is revealed in a dialog that holds the page
 * until it is saved.
 */
export function AdminInvitationForm() {
  const { t, i18n } = useLingui();
  useConsoleCrumb(
    t({ id: "admin.invitations.new.title", message: "New invitation" }),
  );
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const groups = useQuery(quietQuery(groupsQueryOptions()));
  const providers = useQuery(quietQuery(identityProvidersQueryOptions()));
  const create = useMutation(
    withInlineReport(createInvitationMutationOptions(queryClient), true),
  );
  const [issued, setIssued] = useState<{
    url: string;
    expiresAt: string;
  } | null>(null);

  const groupOptions: EntityOption[] = (groups.data ?? [])
    // A rule group's membership is decided by its rule, so an invitation can
    // only carry manual ones; the server rejects the others.
    .filter((group: AppGroupView) => group.kind === "manual")
    .map((group: AppGroupView) => ({
      id: String(group.id),
      label: group.displayName,
      identifier: group.slug,
    }));

  const providerOptions: EntityOption[] = (providers.data?.items ?? [])
    .filter(
      (provider) =>
        !provider.disabled && provider.ready && provider.mode !== "link_only",
    )
    .map((provider) => ({
      id: provider.slug,
      label: provider.displayName,
      description:
        provider.protocol === "oidc" ||
        provider.protocol === "steam" ||
        provider.protocol === "vrchat"
          ? i18n._(providerProtocolLabel(provider.protocol))
          : provider.protocol,
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
        const response = await create.mutateAsync(body);
        setIssued({ url: response.url, expiresAt: response.expiresAt });
      } catch (failure) {
        if (isCancellation(failure)) return;
        applyServerError(form, failure, {
          locations: { username: "username" },
          codes: { upstream_idp_link_only: "expectedUpstreamIdpSlug" },
        });
      }
    },
  });
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  // A sign-in-off provider enforces the local-credential requirement no
  // matter what the request said: the switch is driven on and locked while one
  // is selected. Moving to a provider that does not enforce releases it again —
  // the on state was the provider's doing, not the operator's choice.
  const chosenSlug = useStore(
    form.store,
    (state) => state.values.expectedUpstreamIdpSlug,
  );
  const enforced = providersById.get(chosenSlug.trim())?.allowSignIn === false;
  const wasEnforced = useRef(false);
  // An effect, not render: setting a field while the form renders updates a
  // component that is mid-render.
  useEffect(() => {
    if (enforced === wasEnforced.current) return;
    wasEnforced.current = enforced;
    if (enforced) {
      form.setFieldValue("requireLocalCredential", true, {
        dontUpdateMeta: true,
      });
    } else {
      form.resetField("requireLocalCredential");
    }
  }, [enforced, form]);

  return (
    <>
      <PageBody>
        <PageHeader
          title={<Trans id="admin.invitations.new.title">New invitation</Trans>}
          description={
            <Trans id="admin.invitations.new.description">
              Set the new account's role, username, groups and sign-in
              requirements. Creating the invitation generates a registration
              link to send to the invitee.
            </Trans>
          }
        />
        <form.AppForm>
          <form.Form
            label={t({
              id: "admin.invitations.new.form",
              message: "New invitation",
            })}
            className="flex flex-col gap-6"
          >
            <form.FormError />

            <Section
              id="role"
              icon={<ShieldIcon />}
              title={<Trans id="admin.invitations.new.role">Role</Trans>}
              description={
                <Trans id="admin.invitations.new.role.description">
                  Choose the new account's role.
                </Trans>
              }
            >
              <form.AppField name="role">
                {(field) => (
                  <RadioGroup
                    aria-label={t({
                      id: "admin.invitations.new.role",
                      message: "Role",
                    })}
                    value={field.state.value}
                    disabled={submitting}
                    onValueChange={(next) => field.handleChange(String(next))}
                  >
                    <ChoiceTile
                      layout="list"
                      value="user"
                      label={
                        <Trans id="admin.invitations.role.user">User</Trans>
                      }
                      description={
                        <Trans id="admin.invitations.new.role.user.description">
                          An ordinary account for signing in to applications.
                        </Trans>
                      }
                    />
                    <ChoiceTile
                      layout="list"
                      value="admin"
                      label={
                        <Trans id="admin.invitations.role.admin">Admin</Trans>
                      }
                      description={
                        <Trans id="admin.invitations.new.role.admin.description">
                          Can manage accounts, applications, identity providers
                          and settings in this console.
                        </Trans>
                      }
                    />
                  </RadioGroup>
                )}
              </form.AppField>
            </Section>

            <Section
              id="account"
              icon={<UserRoundIcon />}
              title={
                <Trans id="admin.invitations.new.account.title">Account</Trans>
              }
              description={
                <Trans id="admin.invitations.new.account.description">
                  Set the new account's username and groups.
                </Trans>
              }
            >
              <div className="flex flex-col gap-6">
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
                          Optional. If left empty, the invitee chooses one when
                          registering.
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

                {groups.isError && (
                  <SectionFailure
                    title={
                      <Trans id="admin.invitations.new.groups-failed">
                        The user groups could not be loaded
                      </Trans>
                    }
                    error={groups.error}
                    onRetry={() => void groups.refetch()}
                  />
                )}
                <form.AppField name="groupIds">
                  {(field) => (
                    <field.GroupPicker
                      description={
                        <Trans id="admin.invitations.new.groups.hint">
                          The new account joins these groups when it registers.
                          Only manual groups can be selected.
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
              </div>
            </Section>

            <Section
              id="sign-in"
              icon={<LogInIcon />}
              title={
                <Trans id="admin.invitations.new.sign-in.title">Sign-in</Trans>
              }
              description={
                <Trans id="admin.invitations.new.sign-in.description">
                  Choose the provider the invitee must use, and whether a local
                  sign-in method is required.
                </Trans>
              }
            >
              <div className="flex flex-col gap-6">
                {providers.isError && (
                  <SectionFailure
                    title={
                      <Trans id="admin.invitations.new.providers-failed">
                        The providers could not be loaded
                      </Trans>
                    }
                    error={providers.error}
                    onRetry={() => void providers.refetch()}
                  />
                )}
                <form.AppField name="expectedUpstreamIdpSlug">
                  {(field) => (
                    <field.IdentityProviderPicker
                      description={
                        <Trans id="admin.invitations.new.provider.hint">
                          Optional. The invitee must register through the
                          selected provider. Providers that are disabled, not
                          ready or set to link existing accounts only are not
                          listed.
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
                            Sign-in is turned off for this provider, so a local
                            sign-in method is required.
                          </Trans>
                        ) : (
                          <Trans id="admin.invitations.new.require_local.hint">
                            After verifying with the provider, the invitee must
                            also set up a passkey, or a password and an
                            authenticator, before the account is created.
                          </Trans>
                        )
                      }
                      isDisabled={enforced}
                    />
                  )}
                </form.AppField>
              </div>
            </Section>

            <div className="flex flex-wrap items-center gap-2">
              <form.SubmitButton>
                <Trans id="admin.invitations.new.submit">
                  Create invitation
                </Trans>
              </form.SubmitButton>
              <CancelLink to="/admin/invitations" pending={submitting} />
            </div>
          </form.Form>
        </form.AppForm>
      </PageBody>

      {issued !== null && (
        // Open exactly while the link is held here, so it cannot outlive it.
        <SecretRevealDialog
          text={issued.url}
          filename="prohibitorum-invitation.txt"
          copy={invitationLinkCopyWithExpiry(
            new Intl.DateTimeFormat(i18n.locale, {
              dateStyle: "medium",
              timeStyle: "short",
            }).format(new Date(issued.expiresAt)),
          )}
          onContinue={async () => {
            await navigate({ to: "/admin/invitations" });
          }}
        />
      )}
    </>
  );
}
