import {
  Checkbox,
  CheckboxGroup,
  Description,
  FieldError,
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
import { useStore } from "@tanstack/react-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Ban, Ticket } from "lucide-react";
import { Fragment, useId, useRef, useState } from "react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import {
  type CreateTokenInput,
  createTokenMutationOptions,
  revokeTokenMutationOptions,
} from "@/api/mutations";
import {
  forwardAuthAppsQueryOptions,
  sessionQueryOptions,
  tokensQueryOptions,
} from "@/api/queries";
import { runWithSudo } from "@/api/sudo";
import { sudoReason } from "@/api/sudo-reasons";
import { Button } from "@/components/custom/Button";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { FormMessages } from "@/components/custom/FormMessages";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { RelativeTime } from "@/components/custom/RelativeTime";
import { SecretReveal } from "@/components/custom/SecretReveal";
import { Section } from "@/components/custom/Section";
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
const chooseApp = msg({
  id: "security.tokens.apps.required",
  message: "Choose at least one application, or pick another level of access.",
});
const noApps = msg({
  id: "security.tokens.no_forward_auth",
  message: "You have no applications to choose from.",
});

/**
 * The levels in the order they include one another. `adminDescription` is
 * what the level means for an administrator, whose full-access token also
 * reaches what they manage.
 */
const accessOptions: {
  value: TokenAccess;
  label: MessageDescriptor;
  description: MessageDescriptor;
  adminDescription?: MessageDescriptor;
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
        "Applications and your account. Anything that asks you to confirm it is you is refused.",
    }),
    adminDescription: msg({
      id: "security.tokens.access.full.hint.admin",
      message:
        "Applications, your account and what you manage as an administrator. Anything that asks you to confirm it is you is refused.",
    }),
  },
  {
    value: "sudo",
    label: msg({
      id: "security.tokens.access.sudo",
      message: "Full access, without confirming it is you",
    }),
    description: msg({
      id: "security.tokens.access.sudo.hint",
      message:
        "Everything, including changing how you sign in, without being asked to confirm it is you.",
    }),
    adminDescription: msg({
      id: "security.tokens.access.sudo.hint.admin",
      message:
        "Everything, including what you manage as an administrator and changing how you sign in, without being asked to confirm it is you.",
    }),
  },
];

/**
 * Personal access tokens.
 *
 * A token has one access level: the applications chosen, every application you
 * can use, full access, or full access that also skips confirming it is you.
 * The chosen applications come from `GET /me/forward-auth-apps`, so a caller
 * picks from what they can use rather than typing an identifier.
 *
 * A failed write is reported by the console's error toast, which the query
 * client raises for every mutation, so the panel draws no notice of its own.
 */
export function TokensPanel() {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const tokens = useQuery(tokensQueryOptions());
  const apps = useQuery(forwardAuthAppsQueryOptions());
  const session = useQuery(sessionQueryOptions());
  const [plaintext, setPlaintext] = useState<string | null>(null);
  const [target, setTarget] = useState<Token | null>(null);
  const [creating, setCreating] = useState(false);
  // Where focus goes back to once the token has been read: the dialog it was
  // revealed in took over from the create dialog, which is gone by then.
  const createButton = useRef<HTMLButtonElement>(null);
  const revoke = useMutation(revokeTokenMutationOptions(queryClient));

  const format = (value?: string) =>
    value === undefined
      ? null
      : new Intl.DateTimeFormat(i18n.locale, {
          dateStyle: "medium",
        }).format(new Date(value));

  return (
    <>
      <Section
        title={<Trans id="security.section.tokens">Access tokens</Trans>}
        action={
          <Button ref={createButton} onPress={() => setCreating(true)}>
            <Trans id="security.tokens.create.open">Create a token</Trans>
          </Button>
        }
      >
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
            const name = token.name;
            return (
              <ItemListRow
                key={token.id}
                icon={<Ticket size={18} aria-hidden="true" />}
                title={name}
                badges={<TokenAccessBadge token={token} />}
                details={[
                  <span key="hint" className="font-mono">
                    {token.tokenHint}
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
                      message: `Revoke ${name}`,
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
        apps={apps.data}
        isAdmin={session.data?.role === "admin"}
        isOpen={creating}
        onOpenChange={setCreating}
        onCreated={(token) => {
          setCreating(false);
          setPlaintext(token);
        }}
      />

      {plaintext !== null && (
        // Open exactly while the plaintext is held here, so it cannot outlive
        // it and only the reveal's own continue control can close it.
        <Modal isOpen onOpenChange={() => {}}>
          <Modal.Backdrop isDismissable={false} isKeyboardDismissDisabled>
            <Modal.Container placement="center" size="lg" scroll="inside">
              <Modal.Dialog>
                <Modal.Header>
                  <Modal.Heading>{t(accessTokenCopy.title)}</Modal.Heading>
                </Modal.Header>
                <SecretReveal
                  text={plaintext}
                  filename="prohibitorum-access-token.txt"
                  copy={accessTokenCopy}
                  onContinue={async () => {
                    setPlaintext(null);
                    requestAnimationFrame(() => createButton.current?.focus());
                  }}
                  inDialog
                />
              </Modal.Dialog>
            </Modal.Container>
          </Modal.Backdrop>
        </Modal>
      )}

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
          revoke.mutate(target.id, { onSettled: () => setTarget(null) });
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
 * Asks for everything a new token needs, in a dialog over the list it will
 * join. The form lives inside the dialog, which unmounts its content when it
 * closes, so every way of closing it — Cancel, Escape, the backdrop, or a
 * token made — starts the next one from a blank form.
 */
function CreateTokenDialog({
  apps,
  isAdmin,
  isOpen,
  onOpenChange,
  onCreated,
}: {
  /** Undefined while the list is still being read. */
  apps: ForwardAuthApp[] | undefined;
  isAdmin: boolean;
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (token: string) => void;
}) {
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
            <CreateTokenForm
              apps={apps}
              isAdmin={isAdmin}
              onCancel={() => onOpenChange(false)}
              onCreated={onCreated}
            />
          </Modal.Dialog>
        </Modal.Container>
      </Modal.Backdrop>
    </Modal>
  );
}

/**
 * The form element spans the body and the footer rather than the body alone:
 * the submit button belongs in the footer, and only the body should scroll when
 * a long application list does not fit.
 *
 * A token that skips confirming it is you is confirmed when the form is
 * submitted: the checked request is kept here and the dialog sends it, closing
 * once the write settles. Cancelling leaves the form as it was, unsent.
 */
function CreateTokenForm({
  apps,
  isAdmin,
  onCancel,
  onCreated,
}: {
  apps: ForwardAuthApp[] | undefined;
  isAdmin: boolean;
  onCancel: () => void;
  onCreated: (token: string) => void;
}) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const create = useMutation(createTokenMutationOptions(queryClient));
  // The request the sudo confirmation is about. It outlives the dialog's
  // closing so the dialog does not change while it fades out.
  const [request, setRequest] = useState<CreateTokenInput | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [sending, setSending] = useState(false);
  const available = apps ?? [];
  // Only a list that has been read and is empty rules the level out; one still
  // on its way leaves it open.
  const noneAvailable = apps !== undefined && apps.length === 0;

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

  const form = useAppForm({
    defaultValues: {
      name: "",
      expiresInDays: "0",
      access: "all_apps" as TokenAccess,
      appClientIds: [] as string[],
    },
    // The name and the application list check themselves: a validator runs
    // on submit as well, and an invalid field keeps this from being called.
    onSubmit: async ({ value }) => {
      const name = value.name.trim();
      const body = buildTokenRequest({ ...value, name });
      if (value.access === "sudo") {
        setRequest(body);
        setConfirming(true);
        return;
      }
      await send(body);
    },
  });

  async function send(body: CreateTokenInput) {
    setSending(true);
    try {
      const created = await runWithSudo(
        () => create.mutateAsync(body),
        sudoReason.createToken,
      );
      onCreated(created.token);
    } catch (failure) {
      if (isCancellation(failure)) return;
      applyServerError(form, failure, {
        locations: { name: "name" },
        codes: {},
      });
    } finally {
      setSending(false);
    }
  }

  // The application list is checked on submit and mounted only under its own
  // level, so moving to another level takes its refusal away with it.
  function clearAppsError() {
    if (form.getFieldMeta("appClientIds") === undefined) return;
    form.setFieldMeta("appClientIds", (meta) => ({
      ...meta,
      errorMap: { ...meta.errorMap, onSubmit: undefined },
    }));
  }

  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const access = useStore(form.store, (state) => state.values.access);
  const busy = submitting || sending;

  // The applications a `selected_apps` token is limited to, drawn under that
  // level's own option. It sits inside the radio group, whose arrow keys move
  // between the levels, so they are kept from reaching it here.
  const appChecklist = (
    <form.AppField
      name="appClientIds"
      validators={{
        onSubmit: ({ value }) => (value.length === 0 ? chooseApp : undefined),
      }}
    >
      {(field) => {
        const errors: unknown[] = field.state.meta.errors.flat(Infinity);
        const invalid = errors.length > 0;
        return (
          // biome-ignore lint/a11y/noStaticElementInteractions: only stops the radio group's arrow keys from reaching the checkboxes.
          <div
            className="ms-7"
            onKeyDown={(event) => {
              if (event.key.startsWith("Arrow")) event.stopPropagation();
            }}
          >
            <CheckboxGroup
              variant="secondary"
              name={field.name}
              value={field.state.value}
              isDisabled={busy}
              isInvalid={invalid}
              onChange={(next) => field.handleChange(next)}
            >
              {/* Named for assistive technology; on screen the option above
                  already says what the list is. */}
              <Label className="sr-only">
                <Trans id="security.tokens.apps">Applications</Trans>
              </Label>
              {available.map((app) => (
                <Checkbox key={app.clientId} value={app.clientId}>
                  <Checkbox.Content>
                    <Checkbox.Control>
                      <Checkbox.Indicator />
                    </Checkbox.Control>
                    <span className="wrap-anywhere">{app.displayName}</span>
                  </Checkbox.Content>
                </Checkbox>
              ))}
              {invalid && (
                <FieldError>
                  <FormMessages errors={errors} />
                </FieldError>
              )}
            </CheckboxGroup>
          </div>
        );
      }}
    </form.AppField>
  );

  return (
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
                The token itself is shown once, right after you create it. Copy
                it then; the list will only ever show the last few characters.
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
                      What will use this token, so you know what to revoke
                      later.
                    </Trans>
                  }
                  autoComplete="off"
                  variant="secondary"
                />
              )}
            </form.AppField>

            <form.AppField name="expiresInDays">
              {(field) => (
                <Select
                  variant="secondary"
                  name={field.name}
                  value={field.state.value}
                  isDisabled={busy}
                  onChange={(key) => {
                    if (typeof key === "string") field.handleChange(key);
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
              )}
            </form.AppField>

            <form.AppField name="access">
              {(field) => (
                <RadioGroup
                  variant="secondary"
                  name={field.name}
                  value={field.state.value}
                  isDisabled={busy}
                  onChange={(next) => {
                    field.handleChange(next as TokenAccess);
                    clearAppsError();
                  }}
                >
                  <Label>
                    <Trans id="security.tokens.scope">What it may access</Trans>
                  </Label>
                  {accessOptions.map((option) => {
                    const unavailable =
                      option.value === "selected_apps" && noneAvailable;
                    const description = unavailable
                      ? noApps
                      : isAdmin && option.adminDescription !== undefined
                        ? option.adminDescription
                        : option.description;
                    return (
                      <Fragment key={option.value}>
                        <AccessRadio
                          value={option.value}
                          label={i18n._(option.label)}
                          description={i18n._(description)}
                          isDisabled={unavailable}
                        />
                        {option.value === "selected_apps" &&
                          access === "selected_apps" &&
                          !unavailable &&
                          appChecklist}
                      </Fragment>
                    );
                  })}
                </RadioGroup>
              )}
            </form.AppField>
          </div>
        </Modal.Body>

        <Modal.Footer className="mt-5">
          <Button variant="secondary" isDisabled={busy} onPress={onCancel}>
            <Trans id="security.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton tone={access === "sudo" ? "warning" : "default"}>
            <Trans id="security.tokens.create.action">Create token</Trans>
          </form.SubmitButton>
        </Modal.Footer>
      </form.Form>

      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={(open) => {
          if (!open && !sending) setConfirming(false);
        }}
        status="warning"
        title={
          <Trans id="security.tokens.sudo.confirm.title">
            Create a token that skips confirming it is you?
          </Trans>
        }
        body={
          <p>
            <Trans id="security.tokens.sudo.confirm.body">
              Anyone with this token can change your password, passkeys and
              authenticator without being asked to confirm it is you.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="security.tokens.sudo.confirm.action">Create token</Trans>
        }
        isPending={sending}
        onConfirm={() => {
          if (request === null) return;
          // Closed once the write settles either way: a refusal is reported
          // on the form, and a token made closes the form's dialog as well.
          void send(request).finally(() => setConfirming(false));
        }}
      />
    </form.AppForm>
  );
}

/**
 * One access level. The description sits inside the label, stacked under the
 * name as in HeroUI's delivery demo, so pressing it selects the level the way
 * pressing the name does. The radio is named by the level alone; the line
 * under it stays its description rather than joining its name.
 */
function AccessRadio({
  value,
  label,
  description,
  isDisabled,
}: {
  value: TokenAccess;
  label: string;
  description: string;
  isDisabled: boolean;
}) {
  const nameId = useId();
  return (
    <Radio value={value} aria-labelledby={nameId} isDisabled={isDisabled}>
      <Radio.Content className="items-start">
        <Radio.Control className="mt-0.5">
          <Radio.Indicator />
        </Radio.Control>
        <div className="flex min-w-0 flex-col">
          <span id={nameId}>{label}</span>
          <Description>{description}</Description>
        </div>
      </Radio.Content>
    </Radio>
  );
}
