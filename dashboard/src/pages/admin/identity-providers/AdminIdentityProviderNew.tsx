import { Description, Label, Radio, RadioGroup } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { isCancellation } from "@/api/errors";
import {
  defaultOidcProviderConfig,
  readProviderProtocol,
} from "@/api/federation";
import { createIdentityProviderMutationOptions } from "@/api/mutations";
import type {
  ProviderMode,
  ProviderProtocol,
  ProviderWriteBody,
} from "@/api/raw-admin-paths";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { TagListField } from "@/components/custom/TagListField";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { ClientAuthMethodField } from "@/pages/admin/identity-providers/ClientAuthMethodField";
import { ProvisioningModeField } from "@/pages/admin/identity-providers/ProvisioningModeField";
import {
  type ClientAuthMethod,
  clientAuthNeedsSecret,
  providerProtocolOptions,
} from "@/pages/admin/identity-providers/provider-options";
import {
  clientIdProblem,
  issuerProblem,
  scopeProblem,
  scopesProblem,
  tagListValue,
} from "@/pages/admin/identity-providers/provider-validation";

/** `^[a-z0-9](-?[a-z0-9])*$`, at most 64 characters. */
const slugPattern = /^[a-z0-9](-?[a-z0-9])*$/;

const slugInvalid = msg({
  id: "admin.federation.new.slug.invalid",
  message:
    "Use lowercase letters, digits and single hyphens — the identifier appears in the callback address.",
});

/**
 * A new upstream identity provider: OIDC, Steam or VRChat.
 *
 * The protocol decides the rest of the form, because the three do not share a
 * credential. It is fixed once the provider exists — the identifier, the
 * callback address and every linked account hang off it — so it is chosen here,
 * each option with a line saying what it is for, and never edited afterwards.
 *
 * Only what the provider cannot work without is asked for, with the same
 * fields the detail page uses. Everything else — allowed domains, claim
 * mappings, endpoint overrides — keeps the server's default and is changed on
 * the detail page, where the provider's current state is on screen beside it;
 * a new provider always starts on discovery.
 */
export function AdminIdentityProviderNew() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const create = useMutation(
    createIdentityProviderMutationOptions(queryClient),
  );

  const form = useAppForm({
    defaultValues: {
      protocol: "oidc" as ProviderProtocol,
      displayName: "",
      slug: "",
      mode: "auto_provision" as ProviderMode,
      issuerUrl: "",
      clientId: "",
      tokenAuthMethod: "discovery" as ClientAuthMethod,
      clientSecret: "",
      scopes: tagListValue(defaultOidcProviderConfig().scopes),
      webApiKey: "",
    },
    onSubmit: async ({ value }) => {
      const oidc = value.protocol === "oidc";
      const body: ProviderWriteBody = {
        slug: value.slug.trim(),
        displayName: value.displayName.trim(),
        protocol: value.protocol,
        // VRChat links existing accounts and cannot do anything else; the
        // server rejects any other mode for it.
        mode: value.protocol === "vrchat" ? "link_only" : value.mode,
        config: oidc
          ? {
              ...defaultOidcProviderConfig(),
              issuerUrl: value.issuerUrl,
              clientId: value.clientId,
              scopes: value.scopes.tags,
              tokenAuthMethod: value.tokenAuthMethod,
            }
          : {},
        // The server rejects a secret it did not expect: a VRChat provider has
        // none, and a public OIDC client has none either.
        ...(value.protocol === "steam"
          ? { secret: value.webApiKey }
          : oidc && clientAuthNeedsSecret(value.tokenAuthMethod)
            ? { secret: value.clientSecret }
            : {}),
      };

      try {
        const provider = await create.mutateAsync(body);
        await navigate({
          to: "/admin/identity-providers/$slug",
          params: { slug: provider.slug },
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: {
            upstream_idp_already_exists: "slug",
            bad_request: "issuerUrl",
          },
          locations: {},
        });
      }
    },
  });

  const protocol = useStore(form.store, (state) => state.values.protocol);
  const method = useStore(form.store, (state) => state.values.tokenAuthMethod);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  return (
    <ConsoleCard>
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.federation.new.form",
            message: "Add an identity provider",
          })}
        >
          <form.FormError />

          <form.Field name="protocol">
            {(field) => (
              <RadioGroup
                variant="secondary"
                name={field.name}
                value={field.state.value}
                isDisabled={submitting}
                onChange={(next) => {
                  const chosen = readProviderProtocol(next);
                  if (chosen !== undefined) field.handleChange(chosen);
                }}
              >
                <Label>
                  <Trans id="admin.federation.new.protocol">Protocol</Trans>
                </Label>
                {providerProtocolOptions.map((option) => (
                  <Radio key={option.value} value={option.value}>
                    <Radio.Content>
                      <Radio.Control>
                        <Radio.Indicator />
                      </Radio.Control>
                      {i18n._(option.label)}
                    </Radio.Content>
                    <Description>{i18n._(option.description)}</Description>
                  </Radio>
                ))}
              </RadioGroup>
            )}
          </form.Field>

          <form.AppField
            name="displayName"
            validators={{
              onSubmit: ({ value }) =>
                value.trim() === ""
                  ? msg({
                      id: "admin.federation.new.name.required",
                      message: "Enter a name.",
                    })
                  : undefined,
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="admin.federation.new.name">Name</Trans>}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField
            name="slug"
            validators={{
              onSubmit: ({ value }) =>
                value.trim() === ""
                  ? msg({
                      id: "admin.federation.new.slug.required",
                      message: "Enter an identifier.",
                    })
                  : !slugPattern.test(value.trim()) || value.trim().length > 64
                    ? slugInvalid
                    : undefined,
            }}
          >
            {(field) => (
              <field.FormField
                label={<Trans id="admin.federation.new.slug">Identifier</Trans>}
                description={
                  <Trans id="admin.federation.new.slug.hint">
                    Appears in the callback address and cannot be changed later.
                  </Trans>
                }
                isMonospace
                autoComplete="off"
                autoCapitalize="off"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          <form.AppField name="mode">
            {() => <ProvisioningModeField isFixed={protocol === "vrchat"} />}
          </form.AppField>

          {protocol === "oidc" && (
            <>
              <form.AppField
                name="issuerUrl"
                validators={{
                  onSubmit: ({ value }) => issuerProblem(value, false),
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="admin.federation.new.issuer">Issuer URL</Trans>
                    }
                    inputMode="url"
                    isMonospace
                    autoComplete="off"
                    spellCheck={false}
                    placeholder="https://idp.example.com"
                    variant="secondary"
                  />
                )}
              </form.AppField>

              <form.AppField
                name="clientId"
                validators={{ onSubmit: ({ value }) => clientIdProblem(value) }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="admin.federation.new.client-id">
                        Client ID
                      </Trans>
                    }
                    isMonospace
                    autoComplete="off"
                    spellCheck={false}
                    variant="secondary"
                  />
                )}
              </form.AppField>

              <form.AppField name="tokenAuthMethod">
                {() => <ClientAuthMethodField />}
              </form.AppField>

              {clientAuthNeedsSecret(method) && (
                <form.AppField
                  name="clientSecret"
                  validators={{
                    onSubmit: ({ value }) =>
                      value.trim() === ""
                        ? msg({
                            id: "admin.federation.new.secret.required",
                            message: "Enter the client secret.",
                          })
                        : undefined,
                  }}
                >
                  {(field) => (
                    <field.FormField
                      label={
                        <Trans id="admin.federation.new.secret">
                          Client secret
                        </Trans>
                      }
                      type="password"
                      autoComplete="new-password"
                      variant="secondary"
                    />
                  )}
                </form.AppField>
              )}

              <form.AppField
                name="scopes"
                validators={{ onSubmit: ({ value }) => scopesProblem(value) }}
              >
                {() => (
                  <TagListField
                    label={t({
                      id: "admin.federation.connection.scopes",
                      message: "Scopes",
                    })}
                    description={
                      <Trans id="admin.federation.scopes.hint">
                        The provider must support every scope listed here.
                      </Trans>
                    }
                    placeholder="offline_access"
                    addLabel={<Trans id="form.tags.add">Add</Trans>}
                    check={scopeProblem}
                    lockedTags={["openid"]}
                  />
                )}
              </form.AppField>
            </>
          )}

          {protocol === "steam" && (
            <form.AppField
              name="webApiKey"
              validators={{
                onSubmit: ({ value }) =>
                  value.trim() === ""
                    ? msg({
                        id: "admin.federation.new.steam-key.required",
                        message: "Enter the Web API key.",
                      })
                    : undefined,
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="admin.federation.new.steam-key">
                      Web API key
                    </Trans>
                  }
                  type="password"
                  autoComplete="new-password"
                  variant="secondary"
                />
              )}
            </form.AppField>
          )}

          <form.SubmitButton>
            <Trans id="admin.federation.new.submit">Add provider</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}
