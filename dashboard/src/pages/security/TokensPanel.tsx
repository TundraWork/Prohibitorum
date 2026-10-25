import type { MessageDescriptor } from "@lingui/core";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  type UseMutationResult,
  useMutation,
  useQuery,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { Ban, KeyRound, Ticket } from "lucide-react";
import { useId, useRef, useState } from "react";
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
import { quietQuery, withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { FormMessages } from "@/components/custom/FormMessages";
import { ItemList, ItemListRow } from "@/components/custom/ItemList";
import { SecretRevealDialog } from "@/components/custom/SecretReveal";
import { AsyncSection, Section } from "@/components/custom/Section";
import { accessTokenCopy } from "@/components/custom/secret-reveal-copy";
import { TableEmptyState } from "@/components/custom/TableEmptyState";
import {
  TokenAccessBadge,
  TokenAccessSummary,
  TokenExpiry,
  TokenLastUsed,
} from "@/components/custom/TokenAccess";
import { Checkbox } from "@/components/ui/checkbox";
import { CheckboxGroup } from "@/components/ui/checkbox-group";
import {
  Dialog,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogPanel,
  DialogPopup,
  DialogTitle,
} from "@/components/ui/dialog";
import { Field, FieldLabel } from "@/components/ui/field";
import { Label } from "@/components/ui/label";
import { RadioGroup } from "@/components/ui/radio-group";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

type Token = components["schemas"]["PersonalAccessTokenView"];

const nameRequired = msg({
  id: "security.tokens.name.required",
  message: "Enter a name for the token.",
});
const chooseApp = msg({
  id: "security.tokens.apps.required",
  message:
    "Select at least one application, or choose a different access level.",
});
const appsFailedMessage = msg({
  id: "security.tokens.apps_failed",
  message:
    "Could not load your applications. Close this dialog and open it again.",
});
const noApps = msg({
  id: "security.tokens.no_forward_auth",
  message: "No applications are available to choose from.",
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
      message: "Can be used only with the selected applications.",
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
      message: "Can be used with every application you have access to.",
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
        "Access to applications and your account, except actions that require verifying your identity.",
    }),
    adminDescription: msg({
      id: "security.tokens.access.full.hint.admin",
      message:
        "Access to applications, your account and what you manage as an administrator, except actions that require verifying your identity.",
    }),
  },
  {
    value: "sudo",
    label: msg({
      id: "security.tokens.access.sudo",
      message: "Full access without verifying your identity",
    }),
    description: msg({
      id: "security.tokens.access.sudo.hint",
      message:
        "All actions, including changing how you sign in, without verifying your identity.",
    }),
    adminDescription: msg({
      id: "security.tokens.access.sudo.hint.admin",
      message:
        "All actions, including what you manage as an administrator and changing how you sign in, without verifying your identity.",
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
 * A failed revoke is reported by the console's error toast, which the query
 * client raises for every mutation; a failed create is drawn in its dialog.
 */
export function TokensPanel() {
  return (
    <AsyncSection
      resetKey="tokens"
      id="security-section-tokens"
      icon={<KeyRound />}
      title={<TokensTitle />}
      description={<TokensDescription />}
    >
      <TokenList />
    </AsyncSection>
  );
}

function TokensTitle() {
  return <Trans id="security.section.tokens">Access tokens</Trans>;
}

function TokensDescription() {
  return (
    <Trans id="security.tokens.description">
      Create tokens that let scripts and tools act as you. Each token is shown
      only once, when you create it.
    </Trans>
  );
}

function TokenList() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const { data: tokens } = useSuspenseQuery(quietQuery(tokensQueryOptions()));
  // Only fills the create dialog: a failed read is told there, not as a toast.
  const apps = useQuery(quietQuery(forwardAuthAppsQueryOptions()));
  const session = useQuery(sessionQueryOptions());
  const [plaintext, setPlaintext] = useState<string | null>(null);
  const [target, setTarget] = useState<Token | null>(null);
  const [creating, setCreating] = useState(false);
  // Where focus goes back to once the token has been read: the dialog it was
  // revealed in took over from the create dialog, which is gone by then.
  const createButton = useRef<HTMLButtonElement>(null);
  const revoke = useMutation(revokeTokenMutationOptions(queryClient));

  return (
    <>
      <Section
        id="security-section-tokens"
        icon={<KeyRound />}
        title={<TokensTitle />}
        description={<TokensDescription />}
        action={
          <Button
            ref={createButton}
            variant="outline"
            onClick={() => setCreating(true)}
          >
            <Trans id="security.tokens.create.open">Create a token</Trans>
          </Button>
        }
      >
        <ItemList
          label={t({ id: "security.tokens.table", message: "Access tokens" })}
          empty={
            <TableEmptyState
              icon={<Ticket size={18} strokeWidth={1.75} aria-hidden="true" />}
              title={
                <Trans id="security.tokens.empty">No access tokens yet</Trans>
              }
            />
          }
        >
          {tokens.map((token) => {
            const name = token.name;
            return (
              <ItemListRow
                key={token.id}
                icon={<Ticket aria-hidden="true" />}
                title={name}
                // Beside the name rather than in the badge column: the column is
                // narrower than this label.
                badges={<TokenAccessBadge token={token} />}
                meta={<TokenExpiry token={token} />}
                details={[
                  <span key="hint" className="font-mono">
                    {token.tokenHint}
                  </span>,
                  <TokenAccessSummary key="access" token={token} />,
                  <TokenLastUsed key="used" token={token} />,
                ]}
                actions={
                  <Button
                    aria-label={t({
                      id: "security.tokens.revoke",
                      message: `Revoke ${name}`,
                    })}
                    onClick={() => setTarget(token)}
                    variant="destructive-outline"
                    size="icon-sm"
                  >
                    <Ban aria-hidden="true" />
                  </Button>
                }
              />
            );
          })}
        </ItemList>
      </Section>

      <CreateTokenDialog
        apps={apps.data}
        appsFailed={apps.isError}
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
        <SecretRevealDialog
          text={plaintext}
          filename="prohibitorum-access-token.txt"
          copy={accessTokenCopy}
          onContinue={async () => {
            setPlaintext(null);
            requestAnimationFrame(() => createButton.current?.focus());
          }}
        />
      )}

      <ConfirmDialog
        isOpen={target !== null}
        onOpenChange={(open) => !open && setTarget(null)}
        status="danger"
        title={
          <Trans id="security.tokens.revoke.title">Revoke this token?</Trans>
        }
        subject={target?.name}
        body={
          <p>
            <Trans id="security.tokens.revoke.body">
              The token stops working immediately, and any tool using it loses
              access. This cannot be undone.
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

type CreateToken = UseMutationResult<
  components["schemas"]["PersonalAccessTokenCreated"],
  Error,
  CreateTokenInput
>;

/**
 * Asks for everything a new token needs, in a dialog over the list it will
 * join. The form lives inside the dialog, which unmounts its content when it
 * closes, so every way of closing it — Cancel, Escape, the backdrop, or a
 * token made — starts the next one from a blank form.
 *
 * The write is held here, and the dialog stays up while it runs, so a refusal
 * lands in the form it belongs to.
 */
function CreateTokenDialog({
  apps,
  appsFailed,
  isAdmin,
  isOpen,
  onOpenChange,
  onCreated,
}: {
  /** Undefined while the list is still being read. */
  apps: ForwardAuthApp[] | undefined;
  /** The read failed, so the list is not known to be empty or full. */
  appsFailed: boolean;
  isAdmin: boolean;
  isOpen: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: (token: string) => void;
}) {
  const queryClient = useQueryClient();
  // The form draws each failure, so the toast stays silent.
  const create = useMutation(
    withInlineReport(createTokenMutationOptions(queryClient), true),
  );
  return (
    <Dialog
      open={isOpen}
      onOpenChange={(next) => !next && !create.isPending && onOpenChange(false)}
    >
      <DialogPopup className="sm:max-w-xl">
        <CreateTokenForm
          apps={apps}
          appsFailed={appsFailed}
          isAdmin={isAdmin}
          create={create}
          onCancel={() => onOpenChange(false)}
          onCreated={onCreated}
        />
      </DialogPopup>
    </Dialog>
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
  appsFailed,
  isAdmin,
  create,
  onCancel,
  onCreated,
}: {
  apps: ForwardAuthApp[] | undefined;
  appsFailed: boolean;
  isAdmin: boolean;
  create: CreateToken;
  onCancel: () => void;
  onCreated: (token: string) => void;
}) {
  const { t, i18n } = useLingui();
  const levelId = useId();
  const errorId = useId();
  // The request the sudo confirmation is about. It outlives the dialog's
  // closing so the dialog does not change while it fades out.
  const [request, setRequest] = useState<CreateTokenInput | null>(null);
  const [confirming, setConfirming] = useState(false);
  const [sending, setSending] = useState(false);
  const available = apps ?? [];
  // Only a list that has been read and is empty rules the level out; one still
  // on its way leaves it open.
  const noneAvailable = apps !== undefined && apps.length === 0;
  // A failed read says so rather than claiming there is nothing to choose.
  const cannotChoose = noneAvailable || appsFailed;

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

  // The applications a `selected_apps` token is limited to, drawn in a well
  // under that level's own option. It sits inside the radio group, whose arrow
  // keys move between the levels, so they are kept from reaching it here.
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
            onKeyDown={(event) => {
              if (event.key.startsWith("Arrow")) event.stopPropagation();
            }}
          >
            <CheckboxGroup
              aria-label={t({
                id: "security.tokens.apps",
                message: "Applications",
              })}
              value={field.state.value}
              disabled={busy}
              onValueChange={(next) => field.handleChange(next)}
              aria-describedby={invalid ? errorId : undefined}
              className="gap-2"
            >
              {/* About eight rows show before the list scrolls in place. */}
              <div className="flex max-h-56 w-full flex-col gap-2 overflow-y-auto">
                {available.map((app) => (
                  <Label
                    key={app.clientId}
                    className="w-fit max-w-full text-xs sm:text-xs"
                  >
                    <Checkbox
                      value={app.clientId}
                      aria-invalid={invalid || undefined}
                    />
                    <span className="wrap-anywhere">{app.displayName}</span>
                  </Label>
                ))}
              </div>
              {invalid && (
                <div
                  id={errorId}
                  className="text-destructive-foreground text-xs"
                >
                  <FormMessages errors={errors} />
                </div>
              )}
            </CheckboxGroup>
          </div>
        );
      }}
    </form.AppField>
  );

  return (
    <form.AppForm>
      <DialogHeader>
        <DialogTitle>
          <Trans id="security.tokens.create.title">New access token</Trans>
        </DialogTitle>
        <DialogDescription>
          <Trans id="security.tokens.create.note">
            The token is shown only once, right after you create it. The token
            list shows only prohibitorum_pat_… and the token's last 4
            characters.
          </Trans>
        </DialogDescription>
      </DialogHeader>
      <form.Form
        label={t({
          id: "security.tokens.create.form",
          message: "New access token",
        })}
        className="flex min-h-0 flex-1 flex-col"
      >
        <DialogPanel className="flex flex-col gap-4">
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
                    The tool or purpose this token is for, so you can identify
                    it later.
                  </Trans>
                }
                autoComplete="off"
              />
            )}
          </form.AppField>

          <form.AppField name="expiresInDays">
            {(field) => (
              <Field disabled={busy} name={field.name}>
                <FieldLabel>
                  <Trans id="security.tokens.expiry">Expires after</Trans>
                </FieldLabel>
                <Select
                  value={field.state.value}
                  disabled={busy}
                  onValueChange={(next) => {
                    if (typeof next === "string") field.handleChange(next);
                  }}
                >
                  <SelectTrigger>
                    <SelectValue>
                      {(value: string) =>
                        expiryOptions.find((option) => option.value === value)
                          ?.label
                      }
                    </SelectValue>
                  </SelectTrigger>
                  <SelectPopup alignItemWithTrigger={false}>
                    {expiryOptions.map((option) => (
                      <SelectItem key={option.value} value={option.value}>
                        {option.label}
                      </SelectItem>
                    ))}
                  </SelectPopup>
                </Select>
              </Field>
            )}
          </form.AppField>

          <form.AppField name="access">
            {(field) => (
              <div className="flex flex-col gap-2">
                <span id={levelId} className="font-medium text-sm">
                  <Trans id="security.tokens.scope">Access level</Trans>
                </span>
                <RadioGroup
                  aria-labelledby={levelId}
                  name={field.name}
                  value={field.state.value}
                  disabled={busy}
                  onValueChange={(next) => {
                    field.handleChange(next as TokenAccess);
                    clearAppsError();
                  }}
                >
                  {accessOptions.map((option) => {
                    const unavailable =
                      option.value === "selected_apps" && cannotChoose;
                    const description = unavailable
                      ? appsFailed
                        ? appsFailedMessage
                        : noApps
                      : isAdmin && option.adminDescription !== undefined
                        ? option.adminDescription
                        : option.description;
                    return (
                      <ChoiceTile
                        key={option.value}
                        layout="list"
                        value={option.value}
                        label={i18n._(option.label)}
                        description={i18n._(description)}
                        disabled={unavailable}
                        attached={
                          option.value === "selected_apps" &&
                          access === "selected_apps" &&
                          !unavailable
                            ? appChecklist
                            : undefined
                        }
                      />
                    );
                  })}
                </RadioGroup>
              </div>
            )}
          </form.AppField>
        </DialogPanel>

        <DialogFooter>
          <Button variant="ghost" disabled={busy} onClick={onCancel}>
            <Trans id="security.cancel">Cancel</Trans>
          </Button>
          <form.SubmitButton
            fullWidth
            tone={access === "sudo" ? "destructive" : "default"}
          >
            <Trans id="security.tokens.create.action">Create token</Trans>
          </form.SubmitButton>
        </DialogFooter>
      </form.Form>

      <ConfirmDialog
        isOpen={confirming}
        onOpenChange={(open) => {
          if (!open && !sending) setConfirming(false);
        }}
        status="warning"
        title={
          <Trans id="security.tokens.sudo.confirm.title">
            Create a token that skips verifying your identity?
          </Trans>
        }
        body={
          <p>
            <Trans id="security.tokens.sudo.confirm.body">
              Anyone with this token can change your password, passkeys and
              authenticator without verifying your identity.
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
