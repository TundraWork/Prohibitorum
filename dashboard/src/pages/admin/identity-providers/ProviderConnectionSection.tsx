import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import {
  defaultOidcProviderConfig,
  readOidcProviderConfig,
} from "@/api/federation";
import type { components } from "@/api/generated/schema";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { Section } from "@/components/custom/Section";
import { TagListField } from "@/components/custom/TagListField";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { ClientAuthMethodField } from "@/pages/admin/identity-providers/ClientAuthMethodField";
import { EndpointsField } from "@/pages/admin/identity-providers/EndpointsField";
import {
  type ClientAuthMethod,
  clientAuthNeedsSecret,
} from "@/pages/admin/identity-providers/provider-options";
import {
  clientAuthMethodProblem,
  clientIdProblem,
  endpointProblems,
  endpointsBody,
  endpointsValue,
  issuerProblem,
  scopeProblem,
  scopesProblem,
  tagListValue,
} from "@/pages/admin/identity-providers/provider-validation";

type Provider = components["schemas"]["IdentityProviderView"];

/**
 * Where the provider signs people in, and how this instance talks to it.
 *
 * Read top to bottom in the order the provider's own console lists them: the
 * issuer and client ID to match, how the token request authenticates, the
 * scopes it asks for, and where the endpoints come from. Discovery is the usual
 * case, so the endpoints stay out of the way until the reader overrides one.
 *
 * Every check runs before the save because the server answers each of them with
 * a bare `bad_request`: the reader would be told the form is wrong without being
 * told where. `bad_request` still lands on the issuer, the field most likely to
 * be the one the server could not reach. The PKCE method and the private-network
 * switch are not on this page; they are read from the saved configuration and
 * sent back as they are.
 */
export function ProviderConnectionSection({
  provider,
}: {
  provider: Provider;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    updateIdentityProviderMutationOptions(queryClient),
  );

  const saved =
    readOidcProviderConfig(provider.config) ?? defaultOidcProviderConfig();

  const form = useAppForm({
    defaultValues: {
      issuerUrl: saved.issuerUrl,
      clientId: saved.clientId,
      tokenAuthMethod: saved.tokenAuthMethod as ClientAuthMethod,
      scopes: tagListValue(saved.scopes),
      endpoints: endpointsValue(saved),
    },
    onSubmit: async ({ value }) => {
      try {
        await update.mutateAsync({
          slug: provider.slug,
          body: identityProviderUpdateBody(provider, {
            config: {
              ...saved,
              issuerUrl: value.issuerUrl,
              clientId: value.clientId,
              tokenAuthMethod: value.tokenAuthMethod,
              scopes: value.scopes.tags,
              ...endpointsBody(value.endpoints),
            },
          }),
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          codes: { bad_request: "issuerUrl" },
          locations: {},
        });
      }
    },
  });

  const manual = useStore(
    form.store,
    (state) => state.values.endpoints.mode === "manual",
  );
  const method = useStore(form.store, (state) => state.values.tokenAuthMethod);

  const title = t({
    id: "admin.federation.connection.title",
    message: "Connection",
  });

  return (
    <Section title={title}>
      <ConsoleCard>
        <form.AppForm>
          <form.Form label={title}>
            <form.FormError />

            <form.AppField
              name="issuerUrl"
              validators={{
                onSubmit: ({ value }) =>
                  issuerProblem(value, saved.allowPrivateNetwork),
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="admin.federation.connection.issuer">
                      Issuer URL
                    </Trans>
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
                    <Trans id="admin.federation.connection.client-id">
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
              {() => (
                <ClientAuthMethodField
                  isDiscoveryUnavailable={manual}
                  secretHint={
                    clientAuthNeedsSecret(method) &&
                    !provider.secretConfigured ? (
                      <Trans id="admin.federation.connection.secret-missing">
                        No client secret is set yet. Set one in the danger zone.
                      </Trans>
                    ) : undefined
                  }
                />
              )}
            </form.AppField>

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

            <form.SubmitButton>
              <Trans id="admin.federation.connection.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>
    </Section>
  );
}
