import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { GlobeIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import type { components } from "@/api/generated/schema";
import { updateForwardAuthAppMutationOptions } from "@/api/mutations";
import { forwardAuthAppUpdateBody } from "@/api/update-bodies";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { hostProblem } from "@/pages/admin/forward-auth-apps/forward-auth-validation";

type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/** The form's values for a stored record, also the baseline after a save. */
function serviceValues(app: ForwardAuthApp) {
  return { host: app.forwardAuthHost };
}

/**
 * Which host the application protects.
 *
 * The Client ID is not a field: the server takes it only on create, and the
 * operator's Traefik router is already configured against it, so the header
 * shows it with a copy button and nothing here offers to change it.
 *
 * The hostname is the value the configuration in the proxy section is generated
 * from, so a reader who saves a new one sees that configuration change with it.
 * The server does not check its shape at all, which is why the console does.
 *
 * The form submits through `forwardAuthAppUpdateBody`. `PUT` replaces the
 * record rather than patching it, so every field this section does not draw is
 * carried from the saved view — the body module is where that merge lives, and
 * a section that built it itself is how a field ends up cleared by a save from
 * somewhere else.
 */
export function ForwardAuthServiceSection({ app }: { app: ForwardAuthApp }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateForwardAuthAppMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: serviceValues(app),
    onSubmit: async ({ value }) => {
      const host = value.host.trim();
      const hostIssue = hostProblem(host);
      if (hostIssue !== undefined) {
        form.setFieldMeta("host", (meta) => ({
          ...meta,
          errorMap: { ...meta.errorMap, onSubmit: [hostIssue] },
        }));
        return;
      }

      try {
        const saved = await update.mutateAsync({
          clientId: app.clientId,
          body: forwardAuthAppUpdateBody(app, {
            host,
          }),
        });
        // The stored record is the new baseline, so the section reads as saved.
        form.reset(serviceValues(saved));
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          // A hostname another application already claims arrives as the same
          // duplicate code, and the host is the field it is about.
          codes: { oidc_client_already_exists: "host", bad_request: "host" },
          locations: {},
        });
      }
    },
  });

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const title = t({
    id: "admin.forward-auth-apps.service",
    message: "Service",
  });

  return (
    <form.AppForm>
      <form.Form label={title}>
        <Section
          id="service"
          icon={<GlobeIcon />}
          title={title}
          description={
            <Trans id="admin.forward-auth-apps.service.description">
              The host protected by the gateway. The client ID shown above
              identifies this application and cannot be changed.
            </Trans>
          }
          dirty={dirty}
          footer={
            dirty ? (
              <ConsequenceFooter
                actionLabel={
                  <Trans id="admin.forward-auth-apps.save">Save</Trans>
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
              name="host"
              validators={{
                onSubmit: ({ value }) => hostProblem(value.trim()),
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="admin.forward-auth-apps.field.host">
                      Hostname
                    </Trans>
                  }
                  description={
                    <Trans id="admin.forward-auth-apps.field.host.hint">
                      The host the application is served on, as Traefik matches
                      it, without scheme or port.
                    </Trans>
                  }
                  autoComplete="off"
                  spellCheck={false}
                  isMonospace
                  compactValue
                  placeholder="app.example.com"
                />
              )}
            </form.AppField>
          </div>
        </Section>
      </form.Form>
    </form.AppForm>
  );
}
