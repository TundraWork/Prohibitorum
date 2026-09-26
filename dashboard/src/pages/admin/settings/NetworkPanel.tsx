import { Label, Radio, RadioGroup } from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import { updateClientIpMutationOptions } from "@/api/mutations";
import { clientIpQueryOptions } from "@/api/queries";
import type { ClientIpSettings } from "@/api/raw-admin-paths";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import {
  type ClientIpStrategy,
  clientIpMessages,
  formatProxies,
  headerNameError,
  parseProxies,
} from "@/pages/admin/settings/client-ip-form";

/**
 * How the server finds a request's client address, which every audit entry,
 * session and rate limit records. Behind a proxy the connection's own address
 * is the proxy's, so the address has to come from a header — and a header is
 * only believed from the proxies listed here.
 */
export function NetworkPanel() {
  const { data } = useSuspenseQuery(clientIpQueryOptions());
  return <ClientIpCard key={JSON.stringify(data)} saved={data} />;
}

function ClientIpCard({ saved }: { saved: ClientIpSettings }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateClientIpMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: {
      strategy: saved.strategy as ClientIpStrategy,
      header: saved.header,
      proxies: formatProxies(saved.trustedProxies ?? []),
    },
    validators: {
      // The list is kept while the connection is the source and the field is
      // hidden, and the server checks it all the same; say so here, since
      // there is no field on screen to point at.
      onSubmit: ({ value }) => {
        if (value.strategy !== "direct") return undefined;
        const parsed = parseProxies(value.proxies);
        return parsed.ok || parsed.problem === "required"
          ? undefined
          : t({
              id: "settings.network.proxies.hidden_invalid",
              message:
                "The trusted proxies list has an entry the server will not accept. Choose X-Forwarded-For to correct it.",
            });
      },
    },
    onSubmit: async ({ value }) => {
      // Past the validators, a list that does not parse can only be the empty
      // one the connection strategy does not need.
      const parsed = parseProxies(value.proxies);
      const trustedProxies = parsed.ok ? parsed.proxies : [];
      try {
        await update.mutateAsync({
          strategy: value.strategy,
          header: value.header,
          trustedProxies,
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, { locations: {}, codes: {} });
      }
    },
  });
  const strategy = useStore(form.store, (state) => state.values.strategy);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  return (
    <Section title={<Trans id="settings.network.title">Client IP</Trans>}>
      <ConsoleCard>
        <form.AppForm>
          <form.Form
            label={t({ id: "settings.network.form", message: "Client IP" })}
          >
            <form.FormError />
            <form.Field name="strategy">
              {(field) => (
                <RadioGroup
                  variant="secondary"
                  isDisabled={submitting}
                  value={field.state.value}
                  onChange={(next) =>
                    field.handleChange(next as ClientIpStrategy)
                  }
                >
                  <Label>
                    <Trans id="settings.network.strategy">
                      Read the address from
                    </Trans>
                  </Label>
                  <Radio value="direct">
                    <Radio.Content>
                      <Radio.Control>
                        <Radio.Indicator />
                      </Radio.Control>
                      <Trans id="settings.network.strategy.direct">
                        The connection
                      </Trans>
                    </Radio.Content>
                  </Radio>
                  <Radio value="forwarded">
                    <Radio.Content>
                      <Radio.Control>
                        <Radio.Indicator />
                      </Radio.Control>
                      X-Forwarded-For
                    </Radio.Content>
                  </Radio>
                  <Radio value="header">
                    <Radio.Content>
                      <Radio.Control>
                        <Radio.Indicator />
                      </Radio.Control>
                      <Trans id="settings.network.strategy.header">
                        Another header
                      </Trans>
                    </Radio.Content>
                  </Radio>
                </RadioGroup>
              )}
            </form.Field>

            {strategy === "header" && (
              <form.AppField
                name="header"
                validators={{
                  onBlur: ({ value }) => headerNameError(value),
                  onSubmit: ({ value }) => headerNameError(value),
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="settings.network.header">Header name</Trans>
                    }
                    placeholder="CF-Connecting-IP"
                    autoComplete="off"
                    spellCheck={false}
                    variant="secondary"
                  />
                )}
              </form.AppField>
            )}

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
                    variant="secondary"
                    className="font-mono"
                  />
                )}
              </form.AppField>
            )}

            <form.SubmitButton>
              <Trans id="settings.network.save">Save</Trans>
            </form.SubmitButton>
          </form.Form>
        </form.AppForm>
      </ConsoleCard>
    </Section>
  );
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
