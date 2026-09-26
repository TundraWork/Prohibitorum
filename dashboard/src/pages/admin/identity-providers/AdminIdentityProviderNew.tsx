import {
  Description,
  Label,
  ListBox,
  Radio,
  RadioGroup,
  Select,
  TextArea,
  TextField,
} from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { type ReactNode, useId, useState } from "react";
import { isCancellation } from "@/api/errors";
import { defaultOidcProviderConfig } from "@/api/federation";
import { createIdentityProviderMutationOptions } from "@/api/mutations";
import type {
  OidcProviderConfig,
  ProviderMode,
  ProviderProtocol,
  ProviderWriteBody,
} from "@/api/raw-admin-paths";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { lineProblemMessage, parseLines } from "@/forms/lines";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

/** `^[a-z0-9](-?[a-z0-9])*$`, at most 64 characters. */
const slugPattern = /^[a-z0-9](-?[a-z0-9])*$/;

const slugInvalid = msg({
  id: "admin.federation.new.slug.invalid",
  message:
    "Use lowercase letters, digits and single hyphens — the identifier appears in the callback address.",
});

/** The provisioning mode as the reader sees it; VRChat's is fixed. */
function modeMessage(mode: ProviderMode) {
  if (mode === "auto_provision") {
    return msg({
      id: "admin.federation.mode.auto",
      message: "Creates accounts",
    });
  }
  if (mode === "invite_only") {
    return msg({
      id: "admin.federation.mode.invite",
      message: "Invitation only",
    });
  }
  return msg({
    id: "admin.federation.mode.link",
    message: "Links existing accounts",
  });
}

const scopesRequired = msg({
  id: "admin.federation.new.scopes.required",
  message: "Enter at least one scope.",
});

/**
 * The provider an invitation-free sign-in goes through: OIDC, Steam or VRChat.
 *
 * The protocol decides the rest of the form, because the three do not share a
 * credential. It is fixed once the provider exists — the slug, the callback
 * address and every linked account hang off it — so it is a choice made here and
 * never edited afterwards.
 *
 * Only what the provider cannot work without is asked for. Everything else —
 * allowed domains, claim mappings, endpoint overrides — keeps the server's
 * default and is changed on the detail page, where the provider's current state
 * is on screen beside it.
 */
export function AdminIdentityProviderNew() {
  const { t, i18n } = useLingui();
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const create = useMutation(
    createIdentityProviderMutationOptions(queryClient),
  );

  const [protocol, setProtocol] = useState<ProviderProtocol>("oidc");
  // VRChat links existing accounts and cannot do anything else, so its mode is
  // not a choice; the server rejects any other value.
  const [mode, setMode] = useState<ProviderMode>("auto_provision");
  const [clientAuthMethod, setClientAuthMethod] =
    useState<OidcProviderConfig["tokenAuthMethod"]>("discovery");
  const [scopesText, setScopesText] = useState("openid\nprofile\nemail");
  // Reported on blur rather than as the reader types: an empty box halfway
  // through retyping the list is not a mistake worth interrupting for.
  const [scopesTouched, setScopesTouched] = useState(false);

  const effectiveMode: ProviderMode =
    protocol === "vrchat" ? "link_only" : mode;

  // What is wrong with the scopes box, if anything: the line problem when there
  // is one, otherwise "you have not entered any".
  function scopesProblem() {
    const parsed = parseLines(scopesText, () => undefined);
    if (!parsed.ok) return lineProblemMessage(parsed.problem);
    return parsed.values.length === 0 ? scopesRequired : undefined;
  }

  const form = useAppForm({
    defaultValues: {
      displayName: "",
      slug: "",
      issuerUrl: "",
      clientId: "",
      clientSecret: "",
      webApiKey: "",
    },
    onSubmit: async ({ value }) => {
      const scopes = parseLines(scopesText, () => undefined);
      setScopesTouched(true);
      if (!scopes.ok || scopes.values.length === 0) return;
      const config =
        protocol === "oidc"
          ? {
              ...defaultOidcProviderConfig(),
              issuerUrl: value.issuerUrl.trim(),
              clientId: value.clientId.trim(),
              scopes: scopes.ok ? scopes.values : [],
              tokenAuthMethod: clientAuthMethod,
            }
          : {};

      const body: ProviderWriteBody = {
        slug: value.slug.trim(),
        displayName: value.displayName.trim(),
        protocol,
        mode: effectiveMode,
        config,
        // The server rejects a secret it did not expect: a VRChat provider has
        // none, and a public OIDC client has none either.
        ...(protocol === "steam"
          ? { secret: value.webApiKey }
          : protocol === "oidc" && clientAuthMethod !== "none"
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

          <RadioGroup
            value={protocol}
            onChange={(next) => setProtocol(next as ProviderProtocol)}
          >
            <Label>
              <Trans id="admin.federation.new.protocol">Protocol</Trans>
            </Label>
            <Radio value="oidc">
              <Radio.Content>
                <Radio.Control>
                  <Radio.Indicator />
                </Radio.Control>
                <Trans id="admin.federation.protocol.oidc">OIDC</Trans>
              </Radio.Content>
            </Radio>
            <Radio value="steam">
              <Radio.Content>
                <Radio.Control>
                  <Radio.Indicator />
                </Radio.Control>
                <Trans id="admin.federation.protocol.steam">Steam</Trans>
              </Radio.Content>
            </Radio>
            <Radio value="vrchat">
              <Radio.Content>
                <Radio.Control>
                  <Radio.Indicator />
                </Radio.Control>
                <Trans id="admin.federation.protocol.vrchat">VRChat</Trans>
              </Radio.Content>
            </Radio>
          </RadioGroup>

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
                autoComplete="off"
                spellCheck={false}
                variant="secondary"
              />
            )}
          </form.AppField>

          {protocol === "vrchat" ? (
            <TextField isReadOnly value={i18n._(modeMessage(effectiveMode))}>
              <Label>
                <Trans id="admin.federation.new.mode">Provisioning</Trans>
              </Label>
            </TextField>
          ) : (
            <RadioGroup
              value={mode}
              onChange={(next) => setMode(next as ProviderMode)}
            >
              <Label>
                <Trans id="admin.federation.new.mode">Provisioning</Trans>
              </Label>
              <Radio value="auto_provision">
                <Radio.Content>
                  <Radio.Control>
                    <Radio.Indicator />
                  </Radio.Control>
                  <Trans id="admin.federation.mode.auto">
                    Creates accounts
                  </Trans>
                </Radio.Content>
              </Radio>
              <Radio value="invite_only">
                <Radio.Content>
                  <Radio.Control>
                    <Radio.Indicator />
                  </Radio.Control>
                  <Trans id="admin.federation.mode.invite">
                    Invitation only
                  </Trans>
                </Radio.Content>
              </Radio>
              <Radio value="link_only">
                <Radio.Content>
                  <Radio.Control>
                    <Radio.Indicator />
                  </Radio.Control>
                  <Trans id="admin.federation.mode.link">
                    Links existing accounts
                  </Trans>
                </Radio.Content>
              </Radio>
            </RadioGroup>
          )}

          {protocol === "oidc" && (
            <>
              <form.AppField
                name="issuerUrl"
                validators={{
                  onSubmit: ({ value }) =>
                    value.trim() === ""
                      ? msg({
                          id: "admin.federation.new.issuer.required",
                          message: "Enter the issuer URL.",
                        })
                      : undefined,
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="admin.federation.new.issuer">Issuer URL</Trans>
                    }
                    autoComplete="off"
                    spellCheck={false}
                    variant="secondary"
                  />
                )}
              </form.AppField>

              <form.AppField
                name="clientId"
                validators={{
                  onSubmit: ({ value }) =>
                    value.trim() === ""
                      ? msg({
                          id: "admin.federation.new.client-id.required",
                          message: "Enter the client ID.",
                        })
                      : undefined,
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="admin.federation.new.client-id">
                        Client ID
                      </Trans>
                    }
                    autoComplete="off"
                    spellCheck={false}
                    variant="secondary"
                  />
                )}
              </form.AppField>

              <Select
                className="w-full"
                variant="secondary"
                value={clientAuthMethod}
                onChange={(key) =>
                  setClientAuthMethod(
                    key as OidcProviderConfig["tokenAuthMethod"],
                  )
                }
              >
                <Label>
                  <Trans id="admin.federation.new.auth-method">
                    Client authentication
                  </Trans>
                </Label>
                <Select.Trigger>
                  <Select.Value />
                  <Select.Indicator />
                </Select.Trigger>
                <Select.Popover>
                  <ListBox>
                    <ListBox.Item id="discovery" textValue="discovery">
                      <Trans id="admin.federation.auth.discovery">
                        Use what discovery reports
                      </Trans>
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                    <ListBox.Item
                      id="client_secret_basic"
                      textValue="client_secret_basic"
                    >
                      client_secret_basic
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                    <ListBox.Item
                      id="client_secret_post"
                      textValue="client_secret_post"
                    >
                      client_secret_post
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                    <ListBox.Item id="none" textValue="none">
                      <Trans id="admin.federation.auth.none">
                        Public client
                      </Trans>
                      <ListBox.ItemIndicator />
                    </ListBox.Item>
                  </ListBox>
                </Select.Popover>
              </Select>

              {clientAuthMethod !== "none" && (
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

          {protocol === "oidc" && (
            <OidcScopesField
              value={scopesText}
              onChange={setScopesText}
              onBlur={() => setScopesTouched(true)}
              description={
                <Trans id="admin.federation.scopes.hint">
                  One per line. The upstream must support every scope you list.
                </Trans>
              }
              error={
                scopesTouched && scopesProblem() !== undefined
                  ? i18n._(scopesProblem() ?? scopesRequired)
                  : undefined
              }
            />
          )}

          <form.SubmitButton>
            <Trans id="admin.federation.new.submit">Add provider</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}

/**
 * The scopes requested from the upstream.
 *
 * One per line rather than a text field holding a space-separated list, because
 * the set is edited by adding and removing entries, and because the line rules
 * are the same ones every other list-shaped field on the console follows —
 * `parseLines` decides what is acceptable, blank lines are separators, and a
 * line with spaces around it is refused rather than trimmed.
 *
 * This is a plain `TextArea` rather than a form field: the value is a single
 * string that the submit handler turns into a scope list, so binding it to a
 * field would mean the form held one thing and sent another.
 */
function OidcScopesField({
  value,
  onChange,
  description,
  onBlur,
  error,
}: {
  value: string;
  onChange: (value: string) => void;
  description: ReactNode;
  onBlur: () => void;
  error?: ReactNode;
}) {
  const id = useId();
  return (
    <div className="flex flex-col gap-1.5">
      <Label htmlFor={id}>
        <Trans id="admin.federation.scopes.label">Scopes</Trans>
      </Label>
      <TextArea
        id={id}
        rows={3}
        spellCheck={false}
        variant="secondary"
        value={value}
        onBlur={onBlur}
        onChange={(event) => onChange(event.target.value)}
      />
      <Description className="text-xs text-muted">{description}</Description>
      {error !== undefined && (
        <span className="text-sm text-danger">{error}</span>
      )}
    </div>
  );
}
