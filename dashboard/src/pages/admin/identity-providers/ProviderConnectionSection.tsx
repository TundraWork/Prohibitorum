import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { PlugIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import {
  defaultOidcProviderConfig,
  readOidcProviderConfig,
} from "@/api/federation";
import type { components } from "@/api/generated/schema";
import {
  invalidateIdentityProviders,
  setIdentityProviderSecretQuietlyMutationOptions,
  updateIdentityProviderMutationOptions,
} from "@/api/mutations";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { Section } from "@/components/custom/Section";
import { TagListField } from "@/components/custom/TagListField";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { CallbackPreview } from "@/pages/admin/identity-providers/CallbackPreview";
import { ClientAuthMethodField } from "@/pages/admin/identity-providers/ClientAuthMethodField";
import { EndpointsField } from "@/pages/admin/identity-providers/EndpointsField";
import { ProviderSecretRow } from "@/pages/admin/identity-providers/ProviderCredentialsSection";
import {
  type ClientAuthMethod,
  clientAuthNeedsSecret,
  type ResponseType,
} from "@/pages/admin/identity-providers/provider-options";
import {
  clientAuthMethodProblem,
  clientIdProblem,
  clientSecretRequired,
  endpointProblems,
  endpointsBody,
  endpointsValue,
  issuerProblem,
  responseTypeProblem,
  scopeProblem,
  tagDraftProblem,
  tagListValue,
} from "@/pages/admin/identity-providers/provider-validation";
import { ResponseTypeField } from "@/pages/admin/identity-providers/ResponseTypeField";

type Provider = components["schemas"]["IdentityProviderView"];

/** The stored connection, which the other sections of the page leave alone. */
function savedConfig(provider: Provider) {
  return readOidcProviderConfig(provider.config) ?? defaultOidcProviderConfig();
}

/** The form's values for a stored record, also the baseline after a save. */
function connectionValues(provider: Provider) {
  const saved = savedConfig(provider);
  return {
    issuerUrl: saved.issuerUrl,
    clientId: saved.clientId,
    tokenAuthMethod: saved.tokenAuthMethod as ClientAuthMethod,
    responseType: (saved.responseType ?? "code") as ResponseType,
    authenticateAvatarFetch: saved.authenticateAvatarFetch,
    clientSecret: "",
    scopes: tagListValue(saved.scopes),
    endpoints: endpointsValue(saved),
  };
}

/**
 * Where the provider signs people in, and how this instance talks to it.
 *
 * Read top to bottom in the order the provider's own console lists them: the
 * issuer and client ID to match, how the token request authenticates and the
 * secret it sends, what the provider sends back, the scopes it asks for, and
 * where the endpoints come from. Discovery is the usual case, so the endpoints
 * stay out of the way until the reader overrides one. The addresses to register
 * with the provider close the section, since they are copied out of it.
 *
 * Every check runs before the save because the server answers each of them with
 * a bare `bad_request`: the reader would be told the form is wrong without being
 * told where. `bad_request` still lands on the issuer, the field most likely to
 * be the one the server could not reach. The PKCE method and the private-network
 * switch are not on this page; they are read from the saved configuration and
 * sent back as they are.
 *
 * The client secret is write-only. A stored one is a row that replaces it from a
 * dialog; one the provider does not hold is asked for in the form, because the
 * chosen method sends it: switching away from a public client draws the field,
 * and saving sends it before the rest of the form. The server takes a secret on
 * its own endpoint only and refuses a configuration that authenticates with a
 * secret it does not have, so the pair cannot land in one request, or in the
 * other order.
 *
 * A failed write is reported once, on the section, and never as a toast too.
 */
export function ProviderConnectionSection({
  provider,
}: {
  provider: Provider;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateIdentityProviderMutationOptions(queryClient), true),
  );
  const setSecret = useMutation(
    withInlineReport(setIdentityProviderSecretQuietlyMutationOptions(), true),
  );

  const saved = savedConfig(provider);

  const form = useAppForm({
    defaultValues: connectionValues(provider),
    onSubmit: async ({ value }) => {
      // The secret goes first, while the stored configuration still names the
      // old method: the rotation endpoint checks the secret against it, and a
      // public client accepts a secret being added ahead of the switch.
      let secretStored = false;
      if (
        clientAuthNeedsSecret(value.tokenAuthMethod) &&
        !provider.secretConfigured
      ) {
        try {
          await setSecret.mutateAsync({
            slug: provider.slug,
            secret: value.clientSecret,
          });
          secretStored = true;
        } catch (error) {
          if (isCancellation(error)) return;
          applyServerError(form, error, {
            codes: { bad_request: "clientSecret" },
            locations: {},
          });
          return;
        }
      }
      try {
        const result = await update.mutateAsync({
          slug: provider.slug,
          body: identityProviderUpdateBody(provider, {
            config: {
              ...saved,
              issuerUrl: value.issuerUrl,
              clientId: value.clientId,
              tokenAuthMethod: value.tokenAuthMethod,
              responseType: value.responseType,
              authenticateAvatarFetch: value.authenticateAvatarFetch,
              scopes: value.scopes.tags,
              ...endpointsBody(value.endpoints),
            },
          }),
        });
        // The stored record is the new baseline, so the section reads as saved.
        form.reset(connectionValues(result));
      } catch (error) {
        if (isCancellation(error)) return;
        // A secret that landed before the failed save stays on the server.
        // The page must re-read the provider, or it keeps asking for what is
        // now stored.
        if (secretStored) {
          await invalidateIdentityProviders(queryClient);
        }
        applyServerError(form, error, {
          codes: { bad_request: "issuerUrl" },
          locations: {},
        });
      }
    },
  });

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const manual = useStore(
    form.store,
    (state) => state.values.endpoints.mode === "manual",
  );
  const method = useStore(form.store, (state) => state.values.tokenAuthMethod);

  const needsSecret = clientAuthNeedsSecret(method);
  // A stored secret is replaced from a row, but only where the stored
  // configuration sends one; choosing a method that sends none leaves it be.
  const replaceable =
    provider.secretConfigured &&
    clientAuthNeedsSecret(saved.tokenAuthMethod) &&
    needsSecret;

  const title = t({
    id: "admin.federation.connection.title",
    message: "Connection",
  });

  return (
    <form.AppForm>
      <form.Form label={title}>
        <Section
          id="connection"
          icon={<PlugIcon />}
          title={title}
          description={
            <Trans id="admin.federation.connection.description">
              Where this provider signs people in, and how this instance talks
              to it.
            </Trans>
          }
          dirty={dirty}
          footer={
            dirty ? (
              <ConsequenceFooter
                actionLabel={
                  <Trans id="admin.federation.connection.save">Save</Trans>
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
              name="issuerUrl"
              validators={{
                onSubmit: ({ value }) =>
                  issuerProblem(value, saved.allowPrivateNetwork),
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
              validators={{ onSubmit: ({ value }) => clientIdProblem(value) }}
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

            <form.AppField
              name="tokenAuthMethod"
              validators={{
                onSubmit: ({ value, fieldApi }) =>
                  clientAuthMethodProblem(
                    value,
                    fieldApi.form.getFieldValue("endpoints").mode,
                    saved.pkceMethod,
                  ),
              }}
            >
              {() => <ClientAuthMethodField isDiscoveryUnavailable={manual} />}
            </form.AppField>

            {needsSecret && !provider.secretConfigured && (
              <form.AppField
                name="clientSecret"
                validators={{
                  onSubmit: ({ value }) =>
                    value.trim() === "" ? clientSecretRequired : undefined,
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
                        The method chosen sends this to the provider; it is
                        stored encrypted and never shown again.
                      </Trans>
                    }
                    type="password"
                    autoComplete="new-password"
                  />
                )}
              </form.AppField>
            )}

            {replaceable && <ProviderSecretRow provider={provider} />}

            <form.AppField
              name="responseType"
              validators={{
                onSubmit: ({ value, fieldApi }) =>
                  responseTypeProblem(
                    value,
                    fieldApi.form.getFieldValue("scopes"),
                    fieldApi.form.getFieldValue("endpoints"),
                  ),
              }}
            >
              {() => <ResponseTypeField />}
            </form.AppField>

            <form.AppField
              name="scopes"
              validators={{ onSubmit: ({ value }) => tagDraftProblem(value) }}
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

            <form.AppField
              name="endpoints"
              validators={{
                onSubmit: ({ value }) => {
                  const problems = endpointProblems(
                    value,
                    saved.allowPrivateNetwork,
                  );
                  return problems.length > 0 ? problems : undefined;
                },
              }}
            >
              {() => <EndpointsField />}
            </form.AppField>

            <div className="border-t pt-6">
              <form.AppField name="authenticateAvatarFetch">
                {(field) => (
                  <field.SwitchField
                    label={
                      <Trans id="admin.federation.connection.avatar-auth">
                        Send the access token when fetching the avatar
                      </Trans>
                    }
                    description={
                      <Trans id="admin.federation.connection.avatar-auth.hint">
                        The provider's access token is sent to whatever avatar
                        URL it returns. Turn this on only if you trust the
                        provider.
                      </Trans>
                    }
                  />
                )}
              </form.AppField>
            </div>

            <div className="border-t pt-6">
              <CallbackPreview slug={provider.slug} />
            </div>
          </div>
        </Section>
      </form.Form>
    </form.AppForm>
  );
}
