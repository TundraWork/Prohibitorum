import { Description, Label, Radio, RadioGroup } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import {
  defaultOidcProviderConfig,
  readOidcProviderConfig,
} from "@/api/federation";
import { updateIdentityProviderMutationOptions } from "@/api/mutations";
import type { OidcProviderConfig, ProviderMode } from "@/api/raw-admin-paths";
import { identityProviderUpdateBody } from "@/api/update-bodies";
import { lineProblemMessage, parseLines } from "@/forms/lines";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type Provider = {
  slug: string;
  displayName: string;
  mode: string;
  config: unknown;
};

const scopesRequired = msg({
  id: "admin.federation.connection.scopes.required",
  message: "Enter at least one scope.",
});

/**
 * Where the upstream signs people in.
 *
 * Discovery and a hand-written configuration are one form, not two. They reach
 * the same four endpoints, and discovery's answers can be overridden field by
 * field, so a provider can move from discovery to manual without retyping what
 * discovery already found.
 *
 * The endpoint configuration is held in component state rather than in the form
 * because it decides which fields are required: a manual configuration without
 * an authorization endpoint is refused by the server, and the reader has to be
 * told which of the four is missing rather than "this form is invalid".
 *
 * The checks mirror `federationoidc.ValidateConfig` and the server's URL rule.
 * They are not a convenience — the server answers every one of them with a bare
 * `bad_request` and no field, so a mistake it catches is one the reader would
 * never have been told about.
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

  const saved = readOidcProviderConfig(provider.config);

  const [configurationMode, setConfigurationMode] = useState<
    "discovery" | "manual"
  >(saved?.configurationMode ?? "discovery");

  const form = useAppForm({
    defaultValues: {
      issuerUrl: saved?.issuerUrl ?? "",
      clientId: saved?.clientId ?? "",
      scopes: (saved?.scopes ?? ["openid", "profile", "email"]).join("\n"),
      authorization: saved?.endpoints.authorization ?? "",
      token: saved?.endpoints.token ?? "",
      userinfo: saved?.endpoints.userinfo ?? "",
      jwks: saved?.endpoints.jwks ?? "",
    },
    onSubmit: async ({ value }) => {
      const scopes = parseLines(value.scopes, () => undefined);
      if (!scopes.ok || scopes.values.length === 0) return;

      const base = saved ?? defaultOidcProviderConfig();
      const next: OidcProviderConfig = {
        ...base,
        issuerUrl: value.issuerUrl.trim(),
        clientId: value.clientId.trim(),
        scopes: scopes.values,
        configurationMode,
        endpoints: {
          authorization: blankToNull(value.authorization),
          token: blankToNull(value.token),
          userinfo: blankToNull(value.userinfo),
          jwks: blankToNull(value.jwks),
        },
      };

      try {
        await update.mutateAsync({
          slug: provider.slug,
          body: identityProviderUpdateBody(
            {
              slug: provider.slug,
              displayName: provider.displayName,
              protocol: "oidc",
              iconUrl: undefined,
              mode: provider.mode,
              config: provider.config,
              disabled: false,
              secretConfigured: false,
              secretStatus: "",
              secretValidatedAt: null,
              ready: false,
              supportsOperator: false,
              searchFields: [],
              createdAt: "",
            },
            {
              config: next,
              displayName: provider.displayName,
              mode: provider.mode as ProviderMode,
            },
          ),
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

  const manual = configurationMode === "manual";

  return (
    <form.AppForm>
      <form.Form
        label={t({
          id: "admin.federation.connection.form",
          message: "Connection settings",
        })}
      >
        <form.FormError />

        <form.AppField
          name="issuerUrl"
          validators={{
            onSubmit: ({ value }) =>
              value.trim() === ""
                ? msg({
                    id: "admin.federation.connection.issuer.required",
                    message: "Enter the issuer URL.",
                  })
                : undefined,
          }}
        >
          {(field) => (
            <field.FormField
              label={
                <Trans id="admin.federation.connection.issuer">
                  Issuer URL
                </Trans>
              }
              autoComplete="off"
              spellCheck={false}
              variant="secondary"
            />
          )}
        </form.AppField>

        <form.AppField name="clientId">
          {(field) => (
            <field.FormField
              label={
                <Trans id="admin.federation.connection.client-id">
                  Client ID
                </Trans>
              }
              autoComplete="off"
              spellCheck={false}
              variant="secondary"
            />
          )}
        </form.AppField>

        <form.AppField
          name="scopes"
          validators={{
            onSubmit: ({ value }) => {
              const parsed = parseLines(value, () => undefined);
              if (!parsed.ok) return lineProblemMessage(parsed.problem);
              return parsed.values.length === 0 ? scopesRequired : undefined;
            },
          }}
        >
          {(field) => (
            <field.TextAreaField
              label={
                <Trans id="admin.federation.connection.scopes">Scopes</Trans>
              }
              description={
                <Trans id="admin.federation.connection.scopes.hint">
                  One per line.
                </Trans>
              }
              rows={3}
              spellCheck={false}
              variant="secondary"
            />
          )}
        </form.AppField>

        <RadioGroup
          name="configurationMode"
          value={configurationMode}
          variant="secondary"
          onChange={(next) =>
            setConfigurationMode(next as "discovery" | "manual")
          }
        >
          <Label>
            <Trans id="admin.federation.connection.mode">
              Endpoint configuration
            </Trans>
          </Label>
          <Description>
            <Trans id="admin.federation.connection.mode.note">
              Discovery reads the endpoints from the issuer. Anything you enter
              here replaces what it found.
            </Trans>
          </Description>
          <Radio value="discovery">
            <Radio.Content>
              <Radio.Control>
                <Radio.Indicator />
              </Radio.Control>
              <Trans id="admin.federation.connection.mode.discovery">
                Discover automatically
              </Trans>
            </Radio.Content>
          </Radio>
          <Radio value="manual">
            <Radio.Content>
              <Radio.Control>
                <Radio.Indicator />
              </Radio.Control>
              <Trans id="admin.federation.connection.mode.manual">
                Enter the endpoints
              </Trans>
            </Radio.Content>
          </Radio>
        </RadioGroup>

        <form.AppField
          name="authorization"
          validators={{
            onSubmit: ({ value }) =>
              manual && value.trim() === ""
                ? msg({
                    id: "admin.federation.connection.authorization.required",
                    message: "Enter the authorization endpoint.",
                  })
                : undefined,
          }}
        >
          {(field) => (
            <field.FormField
              label={
                <Trans id="admin.federation.connection.authorization">
                  Authorization endpoint
                </Trans>
              }
              autoComplete="off"
              spellCheck={false}
              variant="secondary"
            />
          )}
        </form.AppField>

        <form.AppField
          name="token"
          validators={{
            onSubmit: ({ value }) =>
              manual && value.trim() === ""
                ? msg({
                    id: "admin.federation.connection.token.required",
                    message: "Enter the token endpoint.",
                  })
                : undefined,
          }}
        >
          {(field) => (
            <field.FormField
              label={
                <Trans id="admin.federation.connection.token">
                  Token endpoint
                </Trans>
              }
              autoComplete="off"
              spellCheck={false}
              variant="secondary"
            />
          )}
        </form.AppField>

        <form.AppField name="userinfo">
          {(field) => (
            <field.FormField
              label={
                <Trans id="admin.federation.connection.userinfo">
                  UserInfo endpoint
                </Trans>
              }
              description={
                <Trans id="admin.federation.connection.userinfo.hint">
                  Leave it empty to skip the UserInfo request.
                </Trans>
              }
              autoComplete="off"
              spellCheck={false}
              variant="secondary"
            />
          )}
        </form.AppField>

        <form.AppField
          name="jwks"
          validators={{
            onSubmit: ({ value }) =>
              manual && value.trim() === ""
                ? msg({
                    id: "admin.federation.connection.jwks.required",
                    message: "Enter the JWKS endpoint.",
                  })
                : undefined,
          }}
        >
          {(field) => (
            <field.FormField
              label={
                <Trans id="admin.federation.connection.jwks">
                  JWKS endpoint
                </Trans>
              }
              autoComplete="off"
              spellCheck={false}
              variant="secondary"
            />
          )}
        </form.AppField>

        <form.SubmitButton>
          <Trans id="admin.federation.connection.save">Save</Trans>
        </form.SubmitButton>
      </form.Form>
    </form.AppForm>
  );
}

/** An untouched endpoint field means "no override", which the wire spells `null`. */
function blankToNull(value: string): string | null {
  const trimmed = value.trim();
  return trimmed === "" ? null : trimmed;
}
