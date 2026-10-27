import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { NetworkIcon } from "lucide-react";
import { useId, useState } from "react";
import { isCancellation } from "@/api/errors";
import { updateClientIpMutationOptions } from "@/api/mutations";
import { clientIpQueryOptions } from "@/api/queries";
import type { ClientIpSettings } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { Section } from "@/components/custom/Section";
import { RadioGroup } from "@/components/ui/radio-group";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  type ClientIpStrategy,
  changesClientAddress,
  clientIpMessages,
  formatProxies,
  headerNameError,
  parseProxies,
} from "@/pages/admin/settings/client-ip-form";

/**
 * The section's heading, drawn by the panel and by the boundary that waits for
 * its read, so the two cannot word it differently.
 */
export const clientIpSection = {
  id: "client-ip",
  icon: <NetworkIcon />,
  title: <Trans id="settings.network.title">Client IP</Trans>,
  description: (
    <Trans id="settings.network.description">
      Set where the server reads each request's client address from. The address
      is recorded in audit logs and sessions and used for rate limiting.
    </Trans>
  ),
};

const hiddenListMessage = msg({
  id: "settings.network.proxies.hidden_invalid",
  message:
    "The trusted proxies list contains an invalid entry. Select X-Forwarded-For to correct it.",
});

/** What changing the address does, in the footer. */
function ClientIpConsequence() {
  return (
    <Trans id="settings.network.confirm.body">
      After saving, audit logs, sessions and rate limits use the client address
      read this way for all new requests. An incorrect setting can put all
      requests under one shared rate limit.
    </Trans>
  );
}

/**
 * How the server finds a request's client address, which every audit entry,
 * session and rate limit records. Behind a proxy the connection's own address
 * is the proxy's, so the address has to come from a header — and a header is
 * only believed from the proxies listed here.
 *
 * Changing the source or the proxies moves every later address, so that save
 * states its consequence in the footer and waits for a confirmation.
 */
export function NetworkPanel() {
  const { data } = useSuspenseQuery(clientIpQueryOptions());
  return <ClientIpSection key={JSON.stringify(data)} saved={data} />;
}

function ClientIpSection({ saved }: { saved: ClientIpSettings }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateClientIpMutationOptions(queryClient), true),
  );
  // The checked body the dialog is confirming, held beside the form.
  const [confirming, setConfirming] = useState<ClientIpSettings | null>(null);
  const labelId = useId();

  const save = async (body: ClientIpSettings) => {
    try {
      await update.mutateAsync(body);
      form.reset({
        strategy: body.strategy,
        header: body.header,
        proxies: formatProxies(body.trustedProxies),
      });
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, { locations: {}, codes: {} });
    }
  };

  const form = useAppForm({
    defaultValues: {
      strategy: saved.strategy as ClientIpStrategy,
      header: saved.header,
      proxies: formatProxies(saved.trustedProxies ?? []),
    },
    validators: {
      // The list is kept while the connection is the source and the field is
      // hidden, and the server checks it all the same; the footer says so
      // before the button is pressed, and this is the backstop for a submit
      // that gets past it.
      onSubmit: ({ value }) => hiddenListProblem(value, i18n),
    },
    onSubmit: async ({ value }) => {
      // Past the validators, a list that does not parse can only be the empty
      // one the connection strategy does not need.
      const parsed = parseProxies(value.proxies);
      const body = {
        strategy: value.strategy,
        header: value.header,
        trustedProxies: parsed.ok ? parsed.proxies : [],
      };
      if (changesClientAddress(saved, value)) {
        setConfirming(body);
        return;
      }
      await save(body);
    },
  });

  const values = useStore(form.store, (state) => state.values);
  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const strategy = values.strategy;
  const blocked = hiddenListProblem(values, i18n);
  const consequential = changesClientAddress(saved, values);

  const title = t({ id: "settings.network.form", message: "Client IP" });

  return (
    <>
      <form.AppForm>
        <form.Form label={title}>
          <Section
            {...clientIpSection}
            dirty={dirty}
            footer={
              dirty ? (
                <ConsequenceFooter
                  tone={blocked === undefined ? "warning" : "danger"}
                  consequence={
                    blocked ??
                    (consequential ? <ClientIpConsequence /> : undefined)
                  }
                  actionLabel={
                    consequential ? (
                      <Trans id="settings.network.action-label">
                        Change the client address and save
                      </Trans>
                    ) : (
                      <Trans id="settings.network.save">Save</Trans>
                    )
                  }
                  onDiscard={() => form.reset()}
                  // The confirmation carries the spinner when it sends the save.
                  disabled={blocked !== undefined || update.isPending}
                  pending={submitting}
                />
              ) : undefined
            }
          >
            <div className="flex flex-col gap-6">
              <form.FormError />
              <form.Field name="strategy">
                {(field) => (
                  <div className="flex flex-col gap-2">
                    <span id={labelId} className="font-medium text-sm">
                      <Trans id="settings.network.strategy">
                        Read the address from
                      </Trans>
                    </span>
                    <RadioGroup
                      aria-labelledby={labelId}
                      value={field.state.value}
                      disabled={submitting}
                      onValueChange={(next) =>
                        field.handleChange(next as ClientIpStrategy)
                      }
                    >
                      <ChoiceTile
                        layout="list"
                        value="direct"
                        label={
                          <Trans id="settings.network.strategy.direct">
                            The connection
                          </Trans>
                        }
                        description={
                          <Trans id="settings.network.strategy.direct.description">
                            Use the source address of the connection. For
                            deployments with no proxy in front of the server.
                          </Trans>
                        }
                      />
                      <ChoiceTile
                        layout="list"
                        value="forwarded"
                        label={
                          <span className="font-mono text-[13px]">
                            X-Forwarded-For
                          </span>
                        }
                        description={
                          <Trans id="settings.network.strategy.forwarded.description">
                            Use the address a proxy adds to this header. Only
                            the listed trusted proxies are trusted.
                          </Trans>
                        }
                      />
                      <ChoiceTile
                        layout="list"
                        value="header"
                        label={
                          <Trans id="settings.network.strategy.header">
                            Another header
                          </Trans>
                        }
                        description={
                          <Trans id="settings.network.strategy.header.description">
                            Use the address in a specified header, such as{" "}
                            <code className="whitespace-nowrap font-mono text-[0.9em]">
                              CF-Connecting-IP
                            </code>
                            . Only the listed trusted proxies are trusted.
                          </Trans>
                        }
                        attached={
                          strategy === "header" ? (
                            <form.AppField
                              name="header"
                              validators={{
                                onBlur: ({ value }) => headerNameError(value),
                                onSubmit: ({ value }) => headerNameError(value),
                              }}
                            >
                              {(headerField) => (
                                <headerField.FormField
                                  label={
                                    <Trans id="settings.network.header">
                                      Header name
                                    </Trans>
                                  }
                                  placeholder="CF-Connecting-IP"
                                  autoComplete="off"
                                  spellCheck={false}
                                  isMonospace
                                  compactValue
                                  className="max-w-sm"
                                />
                              )}
                            </form.AppField>
                          ) : undefined
                        }
                      />
                    </RadioGroup>
                  </div>
                )}
              </form.Field>

              {strategy !== "direct" && (
                <form.AppField
                  name="proxies"
                  validators={{
                    onBlur: ({ value }) => proxiesError(i18n, value),
                    onSubmit: ({ value }) => proxiesError(i18n, value),
                  }}
                >
                  {(field) => (
                    <field.TextAreaField
                      label={
                        <Trans id="settings.network.proxies">
                          Trusted proxies
                        </Trans>
                      }
                      description={
                        <Trans id="settings.network.proxies.hint">
                          One address range per line, such as 10.0.0.0/8.
                        </Trans>
                      }
                      rows={5}
                      spellCheck={false}
                      isMonospace
                    />
                  )}
                </form.AppField>
              )}
            </div>
          </Section>
        </form.Form>
      </form.AppForm>

      <ConfirmDialog
        isOpen={confirming !== null}
        onOpenChange={(open) => {
          if (!open && !update.isPending) setConfirming(null);
        }}
        status="warning"
        title={
          <Trans id="settings.network.confirm.title">
            Change the client address?
          </Trans>
        }
        body={
          <p>
            <Trans id="settings.network.confirm.short">
              Client addresses are read the new way immediately after you
              confirm.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="settings.network.confirm.action">Change address</Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (!confirming) return;
          // Closed once the write settles either way: a refusal is reported on
          // the form, where the reader can act on it.
          void save(confirming).finally(() => setConfirming(null));
        }}
      />
    </>
  );
}

/**
 * Why the list cannot be saved while the connection is the source and hides
 * it, or nothing when it can. An empty list is fine there: it is not needed.
 */
function hiddenListProblem(
  value: { strategy: ClientIpStrategy; proxies: string },
  i18n: ReturnType<typeof useLingui>["i18n"],
): string | undefined {
  if (value.strategy !== "direct") return undefined;
  const parsed = parseProxies(value.proxies);
  if (parsed.ok || parsed.problem === "required") return undefined;
  return i18n._(hiddenListMessage);
}

/**
 * What is wrong with the proxies box, naming the line. Returned as text rather
 * than a descriptor because the line number is part of the sentence.
 */
function proxiesError(
  i18n: ReturnType<typeof useLingui>["i18n"],
  value: string,
): string | undefined {
  const parsed = parseProxies(value);
  if (parsed.ok) return undefined;
  const { problem } = parsed;
  if (problem === "required") return i18n._(clientIpMessages.proxiesRequired);
  if (problem === "too-many") return i18n._(clientIpMessages.proxiesTooMany);
  return i18n._({
    ...(problem.kind === "spaces"
      ? clientIpMessages.proxySpaces
      : clientIpMessages.proxyInvalid),
    values: { line: problem.line },
  });
}
