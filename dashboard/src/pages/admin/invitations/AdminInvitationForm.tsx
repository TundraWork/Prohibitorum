import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { LogInIcon, ShieldIcon, UserRoundIcon } from "lucide-react";
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
import { quietQuery, withInlineReport } from "@/app/query-client";
import { CancelLink } from "@/components/custom/CancelLink";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import type { EntityOption } from "@/components/custom/EntityPicker";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { SecretRevealDialog } from "@/components/custom/SecretReveal";
import { Section } from "@/components/custom/Section";
import { invitationLinkCopyWithExpiry } from "@/components/custom/secret-reveal-copy";
import { RadioGroup } from "@/components/ui/radio-group";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

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
 * does not know — so fields left blank are dropped rather than sent empty. And
 * a provider that is disabled or not ready cannot sign anyone in: an invitation
 * bound to one would be un-redeemable, so the picker offers only providers that
 * can actually authenticate.
 *
 * The link comes back once, so it is revealed in a dialog that holds the page
 * until it is saved; the failure of a create is drawn on the form or the field
 * it belongs to, and never also as a toast.
 */
export function AdminInvitationForm() {
  const { t, i18n } = useLingui();
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

  return (
    <>
      <PageBody>
        <PageHeader
          title={<Trans id="admin.invitations.new.title">New invitation</Trans>}
          description={
            <Trans id="admin.invitations.new.description">
              Decide what the new account starts with. You get a link to send to
              the person once you create it.
            </Trans>
          }
        />
        <form.AppForm>
          <form.Form
            label={t({
              id: "admin.invitations.new.form",
              message: "New invitation",
            })}
            className="flex flex-col gap-8"
          >
            <form.FormError />

            <Section
              id="role"
              icon={<ShieldIcon />}
              title={<Trans id="admin.invitations.new.role">Role</Trans>}
              description={
                <Trans id="admin.invitations.new.role.description">
                  What the new account may do once it exists.
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
                          An ordinary account that signs in to applications.
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
                  The name and groups the account is created with.
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
                          Optional. Leave it empty to let the invited person
                          choose their own when they register.
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

                <form.AppField name="groupIds">
                  {(field) => (
                    <field.GroupPicker
                      description={
                        <Trans id="admin.invitations.new.groups.hint">
                          The manual groups the new account joins when it
                          registers.
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
                  Which provider the invited person must use, and whether they
                  also set up a local way to sign in.
                </Trans>
              }
            >
              <div className="flex flex-col gap-6">
                <form.AppField name="expectedUpstreamIdpSlug">
                  {(field) => (
                    <field.IdentityProviderPicker
                      description={
                        <Trans id="admin.invitations.new.provider.hint">
                          Optional. Only providers that are enabled and ready
                          are offered; the invited person must sign in through
                          the one chosen here.
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
