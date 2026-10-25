import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import {
  KeyRoundIcon,
  Link2Icon,
  LogInIcon,
  NetworkIcon,
  PlugIcon,
} from "lucide-react";
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
import { withInlineReport } from "@/app/query-client";
import { CancelLink } from "@/components/custom/CancelLink";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { useConsoleCrumb } from "@/components/custom/console-crumb";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { Section } from "@/components/custom/Section";
import { TagListField } from "@/components/custom/TagListField";
import { RadioGroup } from "@/components/ui/radio-group";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { CallbackPreview } from "@/pages/admin/identity-providers/CallbackPreview";
import { ClientAuthMethodField } from "@/pages/admin/identity-providers/ClientAuthMethodField";
import { ProvisioningModeField } from "@/pages/admin/identity-providers/ProvisioningModeField";
import {
  type ClientAuthMethod,
  clientAuthNeedsSecret,
  providerProtocolOptions,
  type ResponseType,
} from "@/pages/admin/identity-providers/provider-options";
import {
  clientIdProblem,
  issuerProblem,
  responseTypeProblem,
  scopeProblem,
  tagDraftProblem,
  tagListValue,
} from "@/pages/admin/identity-providers/provider-validation";
import { ResponseTypeField } from "@/pages/admin/identity-providers/ResponseTypeField";

/** `^[a-z0-9](-?[a-z0-9])*$`, at most 64 characters. */
const slugPattern = /^[a-z0-9](-?[a-z0-9])*$/;

/** Whether the server will accept `value`, once trimmed, as an identifier. */
function slugAccepted(value: string): boolean {
  const trimmed = value.trim();
  return slugPattern.test(trimmed) && trimmed.length <= 64;
}

const slugInvalid = msg({
  id: "admin.federation.new.slug.invalid",
  message:
    "The identifier is used in the callback addresses and can contain only lowercase letters, digits and single hyphens.",
});

/**
 * A new upstream identity provider: OIDC, Steam or VRChat.
 *
 * The protocol decides the rest of the form, because the three do not share a
 * credential. It is fixed once the provider exists — the identifier, the
 * callback address and every linked account hang off it — so it is chosen here,
 * each option with a line saying what it is for, and never edited afterwards.
 *
 * For OIDC, the identifier is followed by the callback addresses it will
 * produce, so the provider's own registration page can be filled in beside
 * this one. They appear only once the identifier is one the server accepts: an
 * address built from anything else would be registered upstream and never be
 * called.
 *
 * Only what the provider cannot work without is asked for, with the same
 * fields the detail page uses. Everything else — allowed domains, claim
 * mappings, endpoint overrides — keeps the server's default and is changed on
 * the detail page, where the provider's current state is on screen beside it;
 * a new provider always starts on discovery.
 *
 * A failed create is shown on the form or the field it belongs to, and the
 * mutation marks itself inline so the failure is not also a toast.
 */
export function AdminIdentityProviderNew() {
  const { t, i18n } = useLingui();
  useConsoleCrumb(
    t({ id: "admin.federation.new.title", message: "New identity provider" }),
  );
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const create = useMutation(
    withInlineReport(createIdentityProviderMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: {
      protocol: "oidc" as ProviderProtocol,
      displayName: "",
      slug: "",
      mode: "auto_provision" as ProviderMode,
      // VRChat verifies identities by default; everyone else backs a button.
      allowSignIn: true,
      issuerUrl: "",
      clientId: "",
      tokenAuthMethod: "discovery" as ClientAuthMethod,
      responseType: "code" as ResponseType,
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
        // With sign-in off, "creates accounts" cannot be sent: the server
        // rejects the pair and the field no longer offers it.
        mode:
          !value.allowSignIn && value.mode === "auto_provision"
            ? "invite_only"
            : value.mode,
        allowSignIn: value.allowSignIn,
        config: oidc
          ? {
              ...defaultOidcProviderConfig(),
              issuerUrl: value.issuerUrl,
              clientId: value.clientId,
              scopes: value.scopes.tags,
              tokenAuthMethod: value.tokenAuthMethod,
              responseType: value.responseType,
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
  const slug = useStore(form.store, (state) => state.values.slug);
  const method = useStore(form.store, (state) => state.values.tokenAuthMethod);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  return (
    <PageBody>
      <PageHeader
        title={
          <Trans id="admin.federation.new.title">New identity provider</Trans>
        }
        description={
          <Trans id="admin.federation.new.description">
            Add an external identity provider. Its other settings are on the
            provider's page after it is created.
          </Trans>
        }
      />
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.federation.new.form",
            message: "New identity provider",
          })}
          className="flex flex-col gap-6"
        >
          <form.FormError />

          <Section
            id="protocol"
            icon={<NetworkIcon />}
            title={<Trans id="admin.federation.new.protocol">Protocol</Trans>}
            description={
              <Trans id="admin.federation.new.protocol.description">
                The protocol determines which settings the provider needs. It
                can't be changed after the provider is created.
              </Trans>
            }
          >
            <form.Field name="protocol">
              {(field) => (
                <RadioGroup
                  aria-label={t({
                    id: "admin.federation.new.protocol",
                    message: "Protocol",
                  })}
                  name={field.name}
                  value={field.state.value}
                  disabled={submitting}
                  onValueChange={(next) => {
                    const chosen = readProviderProtocol(next);
                    if (chosen === undefined) return;
                    field.handleChange(chosen);
                    // Each protocol carries its own default for the switch:
                    // VRChat verifies identities, the rest back a button.
                    form.setFieldValue("allowSignIn", chosen !== "vrchat");
                    form.setFieldValue(
                      "mode",
                      chosen === "vrchat" ? "invite_only" : "auto_provision",
                    );
                  }}
                >
                  {providerProtocolOptions.map((option) => (
                    <ChoiceTile
                      key={option.value}
                      layout="list"
                      value={option.value}
                      label={i18n._(option.label)}
                      description={i18n._(option.description)}
                    />
                  ))}
                </RadioGroup>
              )}
            </form.Field>
          </Section>

          <Section
            id="provider"
            icon={<LogInIcon />}
            title={<Trans id="admin.federation.new.provider">Provider</Trans>}
            description={
              <Trans id="admin.federation.new.provider.description">
                Set the provider's name and identifier, whether it can be used
                to sign in, and how it handles people without a linked account.
              </Trans>
            }
          >
            <div className="flex flex-col gap-6">
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
                      : !slugAccepted(value)
                        ? slugInvalid
                        : undefined,
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="admin.federation.new.slug">Identifier</Trans>
                    }
                    description={
                      <Trans id="admin.federation.new.slug.hint">
                        Lowercase letters, digits and single hyphens, up to 64
                        characters. Used in the callback addresses and can't be
                        changed after creation.
                      </Trans>
                    }
                    isMonospace
                    compactValue
                    autoComplete="off"
                    autoCapitalize="off"
                    spellCheck={false}
                  />
                )}
              </form.AppField>

              <form.AppField name="allowSignIn">
                {(field) => (
                  <field.SwitchField
                    label={
                      <Trans id="admin.federation.general.sign_in">
                        Allow sign-in
                      </Trans>
                    }
                    description={
                      <>
                        {protocol === "vrchat" && (
                          <span className="mb-1 block">
                            <Trans id="admin.federation.general.sign_in.vrchat_hint">
                              VRChat verifies a profile through a link in its
                              bio, which is weaker than an OIDC sign-in. When
                              this is on, anyone who can edit the profile can
                              sign in to the account linked to it.
                            </Trans>
                          </span>
                        )}
                        <span className="block">
                          <Trans id="admin.federation.general.sign_in.hint">
                            When off, this provider can only be used to link
                            accounts and accept invitations, not to sign in.
                          </Trans>
                        </span>
                      </>
                    }
                  />
                )}
              </form.AppField>

              <form.AppField name="mode">
                {() => (
                  <form.Subscribe
                    selector={(state) =>
                      [state.values.allowSignIn, state.values.mode] as const
                    }
                  >
                    {([allowSignIn, mode]) => (
                      <ProvisioningModeField
                        allowSignIn={allowSignIn}
                        autoProvisionSelected={mode === "auto_provision"}
                        onAutoProvisionInvalid={() => {
                          form.setFieldValue("mode", "invite_only");
                        }}
                      />
                    )}
                  </form.Subscribe>
                )}
              </form.AppField>
            </div>
          </Section>

          {protocol === "oidc" && slugAccepted(slug) && (
            <Section
              id="callback"
              icon={<Link2Icon />}
              title={
                <Trans id="admin.federation.callback.title">
                  Callback addresses
                </Trans>
              }
              description={
                <Trans id="admin.federation.new.callback.description">
                  The addresses on this instance that the provider redirects
                  people back to.
                </Trans>
              }
            >
              <CallbackPreview slug={slug.trim()} titled={false} />
            </Section>
          )}

          {protocol === "oidc" && (
            <Section
              id="connection"
              icon={<PlugIcon />}
              title={
                <Trans id="admin.federation.connection.title">Connection</Trans>
              }
              description={
                <Trans id="admin.federation.connection.description">
                  Set how this instance connects to the provider, such as the
                  issuer URL, client credentials and scopes.
                </Trans>
              }
            >
              <div className="flex flex-col gap-6">
                <form.AppField
                  name="issuerUrl"
                  validators={{
                    onSubmit: ({ value }) => issuerProblem(value, false),
                  }}
                >
                  {(field) => (
                    <field.AddressField
                      label={
                        <Trans id="admin.federation.connection.issuer">
                          Issuer URL
                        </Trans>
                      }
                      placeholder="https://idp.example.com"
                    />
                  )}
                </form.AppField>

                <form.AppField
                  name="clientId"
                  validators={{
                    onSubmit: ({ value }) => clientIdProblem(value),
                  }}
                >
                  {(field) => (
                    <field.FormField
                      label={
                        <Trans id="admin.federation.connection.client-id">
                          Client ID
                        </Trans>
                      }
                      isMonospace
                      compactValue
                      autoComplete="off"
                      spellCheck={false}
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
                        description={
                          <Trans id="admin.federation.connection.secret.hint">
                            Sent to the provider using the selected
                            authentication method. Stored encrypted and never
                            shown again.
                          </Trans>
                        }
                        type="password"
                        autoComplete="new-password"
                      />
                    )}
                  </form.AppField>
                )}

                <form.AppField
                  name="responseType"
                  validators={{
                    onSubmit: ({ value, fieldApi }) =>
                      responseTypeProblem(
                        value,
                        fieldApi.form.getFieldValue("scopes"),
                      ),
                  }}
                >
                  {() => <ResponseTypeField />}
                </form.AppField>

                <form.AppField
                  name="scopes"
                  validators={{
                    onSubmit: ({ value }) => tagDraftProblem(value),
                  }}
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
                      addLabel={t({
                        id: "form.tags.add-scope",
                        message: "Add scope",
                      })}
                      check={scopeProblem}
                    />
                  )}
                </form.AppField>
              </div>
            </Section>
          )}

          {protocol === "steam" && (
            <Section
              id="credentials"
              icon={<KeyRoundIcon />}
              title={
                <Trans id="admin.federation.credentials.steam.title">
                  Credentials
                </Trans>
              }
              description={
                <Trans id="admin.federation.credentials.steam.description">
                  The key is stored encrypted and never shown again.
                </Trans>
              }
            >
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
                  />
                )}
              </form.AppField>
            </Section>
          )}

          <div className="flex flex-wrap items-center gap-2">
            <form.SubmitButton>
              <Trans id="admin.federation.new.submit">Add provider</Trans>
            </form.SubmitButton>
            <CancelLink to="/admin/identity-providers" pending={submitting} />
          </div>
        </form.Form>
      </form.AppForm>
    </PageBody>
  );
}
