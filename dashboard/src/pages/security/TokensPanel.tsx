import {
  Checkbox,
  CheckboxGroup,
  Description,
  Label,
  ListBox,
  Modal,
  Radio,
  RadioGroup,
  Select,
} from "@heroui/react";
import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, Ticket } from "lucide-react";
import { useState } from "react";
import { describeError, isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  type CreateTokenInput,
  createTokenMutationOptions,
  revokeTokenMutationOptions,
} from "@/api/mutations";
import { forwardAuthAppsQueryOptions, tokensQueryOptions } from "@/api/queries";
import { runWithSudo } from "@/api/sudo";
import { sudoReason } from "@/api/sudo-reasons";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SecretReveal } from "@/components/custom/SecretReveal";
import { Section } from "@/components/custom/Section";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { accessTokenCopy } from "@/components/custom/secret-reveal-copy";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import {
  TokenAccessBadge,
  TokenAccessSummary,
} from "@/components/custom/TokenAccess";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type Token = components["schemas"]["PersonalAccessTokenView"];

const nameRequired = msg({
  id: "security.tokens.name.required",
  message: "Give the token a name so you can recognise it later.",
});
const accessOptions: {
  value: TokenAccess;
  label: MessageDescriptor;
  description: MessageDescriptor;
}[] = [
  {
    value: "selected_apps",
    label: msg({
      id: "security.tokens.access.selected_apps",
      message: "Applications you choose",
    }),
    description: msg({
      id: "security.tokens.access.selected_apps.hint",
      message: "Works only on the applications you pick.",
    }),
  },
  {
    value: "all_apps",
    label: msg({
      id: "security.tokens.access.all_apps",
      message: "Every application you can use",
    }),
    description: msg({
      id: "security.tokens.access.all_apps.hint",
      message: "Works on each application you can use.",
    }),
  },
  {
    value: "full",
    label: msg({
      id: "security.tokens.access.full",
      message: "Full access",
    }),
    description: msg({
      id: "security.tokens.access.full.hint",
      message:
        "Applications and your account. Actions that ask you to verify your identity again are refused.",
    }),
  },
  {
    value: "sudo",
    label: msg({
      id: "security.tokens.access.sudo",
      message: "Full access, skipping identity checks",
    }),
    description: msg({
      id: "security.tokens.access.sudo.hint",
      message:
        "Everything, including changing how you sign in, without asking you to verify your identity again.",
    }),
  },
];
const chooseApp = msg({
  id: "security.tokens.apps.required",
  message: "Choose at least one application, or pick another level of access.",
});

/**
 * Personal access tokens.
 *
 * A token has one access level: the applications chosen, every application you
 * can use, full access, or full access that also skips identity checks. The
 * chosen applications come from `GET /me/forward-auth-apps`, so a caller picks
 * from what they can use rather than typing an identifier.
 */
export function TokensPanel() {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const tokens = useQuery(tokensQueryOptions());
  const apps = useQuery(forwardAuthAppsQueryOptions());
  const [plaintext, setPlaintext] = useState<string | null>(null);
  const [target, setTarget] = useState<Token | null>(null);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<unknown>(null);
  const revoke = useMutation(revokeTokenMutationOptions(queryClient));

  const format = (value?: string) =>
    value === undefined
      ? null
      : new Intl.DateTimeFormat(i18n.locale, {
          dateStyle: "medium",
        }).format(new Date(value));

  if (plaintext !== null) {
    return (
      <SecretReveal
        text={plaintext}
        filename="prohibitorum-access-token.txt"
        copy={accessTokenCopy}
        onContinue={async () => {
          setPlaintext(null);
        }}
      />
    );
  }

  return (
    <>
      <Section
        title={<Trans id="security.section.tokens">Access tokens</Trans>}
        action={
          <Button onPress={() => setCreating(true)}>
            <Trans id="security.tokens.create.open">Create a token</Trans>
          </Button>
        }
      >
        {error !== null && (
          <SurfaceAlert status="danger" role="alert">
            <SurfaceAlert.Indicator />
            <SurfaceAlert.Content>
              <SurfaceAlert.Title>{t(describeError(error))}</SurfaceAlert.Title>
            </SurfaceAlert.Content>
          </SurfaceAlert>
        )}

        <ItemList
          label={t({ id: "security.tokens.table", message: "Access tokens" })}
          loading={tokens.isPending}
          empty={
            <TableEmptyState
              icon={<Ticket size={18} strokeWidth={1.75} aria-hidden="true" />}
              title={
                <Trans id="security.tokens.empty">No access tokens yet</Trans>
              }
            />
          }
        >
          {(tokens.data ?? []).map((token) => {
            const expires = format(token.expiresAt);
            const lastUsed = token.lastUsedAt;
            return (
              <ItemListRow
                key={token.id}
                icon={<Ticket size={18} aria-hidden="true" />}
                title={token.name}
                badges={<TokenAccessBadge token={token} />}
                details={[
                  <span key="hint" className="font-mono">
                    …{token.tokenHint}
                  </span>,
                  <TokenAccessSummary key="access" token={token} />,
                  expires === null ? (
                    <Trans key="expires" id="security.tokens.detail.noExpiry">
                      Never expires
                    </Trans>
                  ) : (
                    <Trans key="expires" id="security.tokens.detail.expires">
                      Expires {expires}
                    </Trans>
                  ),
                  lastUsed === undefined ? (
                    <Trans key="used" id="security.tokens.never_used">
                      Not used yet
                    </Trans>
                  ) : (
                    <Trans key="used" id="security.tokens.detail.lastUsed">
                      Last used <RelativeTime value={lastUsed} />
                    </Trans>
                  ),
                ]}
                actions={
                  <Button
                    isIconOnly
                    size="sm"
                    variant="danger-soft"
                    aria-label={t({
                      id: "security.tokens.revoke",
                      message: "Revoke",
                    })}
                    onPress={() => setTarget(token)}
                  >
                    <Ban size={16} aria-hidden="true" />
                  </Button>
                }
              />
            );
          })}
        </ItemList>
      </Section>

      <CreateTokenDialog
        apps={apps.data ?? []}
        isOpen={creating}
        onOpenChange={setCreating}
        onCreated={setPlaintext}
        onError={setError}
      />

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
        status="danger"
        title={
          <Trans id="security.tokens.revoke.title">Revoke this token?</Trans>
        }
        body={
          <p>
            <Trans id="security.tokens.revoke.body">
              Anything using this token stops working immediately. This cannot
              be undone.
            </Trans>
          </p>
        }
        confirmLabel={<Trans id="security.tokens.revoke.action">Revoke</Trans>}
        isPending={revoke.isPending}
        onConfirm={() => {
          if (!target) return;
          revoke.mutate(target.id, {
            onSuccess: () => setTarget(null),
            onError: (failure) => {
              setError(failure);
              setTarget(null);
            },
          });
        }}
      />
    </>
  );
}

export type TokenAccess = Token["access"];

/**
 * Builds the create request. Only a token limited to chosen applications
 * carries a list of them: the server refuses one on any other level, even an
 * empty one. Days of zero or less mean "no expiry", which the server takes as
 * omitted.
 */
export function buildTokenRequest(input: {
  name: string;
  access: TokenAccess;
  appClientIds: readonly string[];
  expiresInDays: string;
}): CreateTokenInput {
  const days = Number(input.expiresInDays);
  return {
    name: input.name,
    access: input.access,
    ...(input.access === "selected_apps"
      ? { appClientIds: [...input.appClientIds] }
      : {}),
    ...(Number.isInteger(days) && days > 0 ? { expiresInDays: days } : {}),
  };
}

type ForwardAuthApp = components["schemas"]["MyForwardAuthApp"];

/**
 * Asks for everything a new token needs, in a dialog over the list it will join.
 * The plaintext stays behind the dialog, so creating a token and reading it
 * remain two steps.
 *
 * The form element spans the body and the footer rather than the body alone:
 * the submit button belongs in the footer, and only the body should scroll when
 * a long application list does not fit.
 */
function CreateTokenDialog({
  apps,
  isOpen,
  onOpenChange,
  onCreated,
  onError,
}: {
  apps: ForwardAuthApp[];
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (token: string) => void;
  onError: (error: unknown) => void;
}) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const [access, setAccess] = useState<TokenAccess>("all_apps");
  const [expiresInDays, setExpiresInDays] = useState("0");
  const [chosen, setChosen] = useState<string[]>([]);

  // The values are the day counts `buildTokenRequest` reads; zero means the
  // server stores no expiry at all.
  const expiryOptions = [
    {
      value: "0",
      label: t({
        id: "security.tokens.expiry.never",
        message: "Never expires",
      }),
    },
    {
      value: "30",
      label: t({ id: "security.tokens.expiry.30", message: "30 days" }),
    },
    {
      value: "90",
      label: t({ id: "security.tokens.expiry.90", message: "90 days" }),
    },
    {
      value: "365",
      label: t({ id: "security.tokens.expiry.365", message: "1 year" }),
    },
  ];

  const create = useMutation(createTokenMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: { name: "" },
    onSubmit: async ({ value }) => {
      const name = value.name.trim();
      if (name === "") {
        form.setFieldMeta("name", (meta) => ({
          ...meta,
          errorMap: { ...meta.errorMap, onSubmit: nameRequired },
        }));
        return;
      }
      if (access === "selected_apps" && chosen.length === 0) {
        form.setErrorMap({ onSubmit: { form: chooseApp, fields: {} } });
        return;
      }
      const body = buildTokenRequest({
        name,
        access,
        appClientIds: chosen,
        expiresInDays,
      });
      try {
        const result = await runWithSudo(
          () => create.mutateAsync(body),
          sudoReason.createToken,
        );
        const created = result as { token?: unknown };
        if (typeof created?.token !== "string") return;
        setChosen([]);
        setAccess("all_apps");
        form.reset();
        onOpenChange(false);
        onCreated(created.token);
      } catch (failure) {
        if (isCancellation(failure)) return;
        applyServerError(form, failure, {
          locations: { name: "name" },
          codes: {},
        });
        onError(failure);
      }
    },
  });

  return (
    <Modal isOpen={isOpen} onOpenChange={onOpenChange}>
      <Modal.Backdrop>
        <Modal.Container placement="center" size="lg">
          <Modal.Dialog>
            <Modal.Header>
              <Modal.Heading>
                <Trans id="security.tokens.create.title">
                  New access token
                </Trans>
              </Modal.Heading>
            </Modal.Header>

            <form.AppForm>
              <form.Form
                label={t({
                  id: "security.tokens.create.form",
                  message: "New access token",
                })}
                className="flex min-h-0 flex-1 flex-col"
              >
                <Modal.Body>
                  <div className="flex flex-col gap-4">
                    <Description>
                      <Trans id="security.tokens.create.note">
                        The token itself is shown once, right after you create
                        it. Copy it then; the list will only ever show the last
                        few characters.
                      </Trans>
                    </Description>

                    <form.FormError />
                    <form.AppField
                      name="name"
                      validators={{
                        onChange: ({ value }) =>
                          value.trim() === "" ? nameRequired : undefined,
                      }}
                    >
                      {(field) => (
                        <field.FormField
                          label={<Trans id="security.tokens.name">Name</Trans>}
                          description={
                            <Trans id="security.tokens.name.hint">
                              What will use this token, so you know what to
                              revoke later.
                            </Trans>
                          }
                          autoComplete="off"
                          variant="secondary"
                        />
                      )}
                    </form.AppField>

                    <Select
                      variant="secondary"
                      value={expiresInDays}
                      onChange={(key) => {
                        if (typeof key === "string") setExpiresInDays(key);
                      }}
                    >
                      <Label>
                        <Trans id="security.tokens.expiry">Expires after</Trans>
                      </Label>
                      <Select.Trigger>
                        <Select.Value />
                        <Select.Indicator />
                      </Select.Trigger>
                      <Select.Popover>
                        <ListBox>
                          {expiryOptions.map((option) => (
                            <ListBox.Item
                              key={option.value}
                              id={option.value}
                              textValue={option.label}
                            >
                              <Label>{option.label}</Label>
                              <ListBox.ItemIndicator />
                            </ListBox.Item>
                          ))}
                        </ListBox>
                      </Select.Popover>
                    </Select>

                    <RadioGroup
                      variant="secondary"
                      name="access"
                      value={access}
                      onChange={(next) => setAccess(next as TokenAccess)}
                    >
                      <Label>
                        <Trans id="security.tokens.scope">
                          What it may access
                        </Trans>
                      </Label>
                      {accessOptions.map((option) => (
                        <Radio key={option.value} value={option.value}>
                          <Radio.Content>
                            <Radio.Control>
                              <Radio.Indicator />
                            </Radio.Control>
                            {i18n._(option.label)}
                          </Radio.Content>
                          <Description>
                            {i18n._(option.description)}
                          </Description>
                        </Radio>
                      ))}
                    </RadioGroup>

                    {access === "selected_apps" &&
                      (apps.length === 0 ? (
                        <p className="text-sm text-muted">
                          <Trans id="security.tokens.no_forward_auth">
                            You have no applications available to grant.
                          </Trans>
                        </p>
                      ) : (
                        <CheckboxGroup
                          variant="secondary"
                          name="appClientIds"
                          value={chosen}
                          onChange={setChosen}
                          className="border-separator border-s ps-4"
                        >
                          <Label>
                            <Trans id="security.tokens.apps">
                              Applications
                            </Trans>
                          </Label>
                          {apps.map((app) => (
                            <Checkbox key={app.clientId} value={app.clientId}>
                              <Checkbox.Content>
                                <Checkbox.Control>
                                  <Checkbox.Indicator />
                                </Checkbox.Control>
                                <span className="wrap-anywhere">
                                  {app.displayName}
                                </span>
                              </Checkbox.Content>
                            </Checkbox>
                          ))}
                        </CheckboxGroup>
                      ))}
                  </div>
                </Modal.Body>

                <Modal.Footer className="mt-5">
                  <Button
                    variant="secondary"
                    isDisabled={create.isPending}
                    onPress={() => onOpenChange(false)}
                  >
                    <Trans id="security.cancel">Cancel</Trans>
                  </Button>
                  <form.SubmitButton
                    tone={access === "sudo" ? "warning" : "default"}
                  >
                    <Trans id="security.tokens.create.action">
                      Create token
                    </Trans>
                  </form.SubmitButton>
                </Modal.Footer>
              </form.Form>
            </form.AppForm>
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}
