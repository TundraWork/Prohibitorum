import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "@tanstack/react-router";
import { AppWindowIcon, GlobeIcon, ShieldCheckIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import { createForwardAuthAppMutationOptions } from "@/api/mutations";
import { withInlineReport } from "@/app/query-client";
import { CancelLink } from "@/components/custom/CancelLink";
import { useConsoleCrumb } from "@/components/custom/console-crumb";
import { PageBody } from "@/components/custom/PageBody";
import { PageHeader } from "@/components/custom/PageHeader";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { hostProblem } from "@/pages/admin/forward-auth-apps/forward-auth-validation";

/**
 * The Client ID rule, identical to the OIDC application's.
 *
 * The server accepts any non-empty string; this refuses one that would not
 * survive the trip. The identifier becomes a path segment in the access and
 * manager endpoints and part of a URL the operator is configured with, so a `?`
 * or a space is a problem to discover later. Leading alphanumeric keeps it out
 * of the reach of a path traversal and of a client that treats a leading dot as
 * a hidden file.
 */
const clientIdPattern = /^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$/;

const clientIdRequired = msg({
  id: "admin.forward-auth-apps.new.client-id.required",
  message: "Enter a Client ID.",
});

const clientIdInvalid = msg({
  id: "admin.forward-auth-apps.new.client-id.invalid",
  message:
    "Use letters, digits, dots, underscores and hyphens, starting with a letter or a digit, up to 128 characters.",
});

const nameRequired = msg({
  id: "admin.forward-auth-apps.new.name.required",
  message: "Enter a name.",
});

/**
 * Registering one forward-auth application.
 *
 * Only what the server cannot default is asked for. The identity projection,
 * the icon and the access policy's groups all have a working default — the
 * record's own — and are set on the detail page, where the application as it
 * currently stands is beside the field. What cannot be defaulted is what the
 * application *is*: its identifier and the host it protects, since the server
 * takes the Client ID and the host only at creation.
 *
 * A failed create is shown on the form or the field it belongs to, and the
 * mutation marks itself inline so the failure is not also a toast.
 */
export function AdminForwardAuthAppNew() {
  const { t } = useLingui();
  useConsoleCrumb(
    t({
      id: "admin.forward-auth-apps.new.title",
      message: "New forward-auth application",
    }),
  );
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const create = useMutation(
    withInlineReport(createForwardAuthAppMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: {
      displayName: "",
      clientId: "",
      host: "",
      accessRestricted: false,
    },
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
        const created = await create.mutateAsync({
          clientId: value.clientId.trim(),
          host,
          displayName: value.displayName.trim() || undefined,
          accessRestricted: value.accessRestricted,
        });
        await navigate({
          to: "/admin/forward-auth-apps/$clientId",
          params: { clientId: created.clientId },
        });
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          // A duplicate Client ID and a duplicate hostname are the same code
          // from the server; the Client ID is the field that names the request,
          // so the message lands there.
          codes: { oidc_client_already_exists: "clientId" },
          locations: {},
        });
      }
    },
  });

  const submitting = useStore(form.store, (state) => state.isSubmitting);

  return (
    <PageBody>
      <PageHeader
        title={
          <Trans id="admin.forward-auth-apps.new.title">
            New forward-auth application
          </Trans>
        }
        description={
          <Trans id="admin.forward-auth-apps.new.description">
            Register a service behind your reverse proxy. Other settings are
            available on the application's page after it is created.
          </Trans>
        }
      />
      <form.AppForm>
        <form.Form
          label={t({
            id: "admin.forward-auth-apps.new.form",
            message: "New forward-auth application",
          })}
          className="flex flex-col gap-6"
        >
          <form.FormError />

          <Section
            id="application"
            icon={<AppWindowIcon />}
            title={
              <Trans id="admin.apps.new.application.title">Application</Trans>
            }
            description={
              <Trans id="admin.forward-auth-apps.new.identity.description">
                The application's display name and identifier.
              </Trans>
            }
          >
            <div className="flex flex-col gap-5">
              <form.AppField
                name="displayName"
                validators={{
                  onSubmit: ({ value }) =>
                    value.trim() === "" ? nameRequired : undefined,
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="admin.forward-auth-apps.field.name">
                        Name
                      </Trans>
                    }
                  />
                )}
              </form.AppField>

              <form.AppField
                name="clientId"
                validators={{
                  onSubmit: ({ value }) => {
                    const clientId = value.trim();
                    if (clientId === "") return clientIdRequired;
                    return clientIdPattern.test(clientId)
                      ? undefined
                      : clientIdInvalid;
                  },
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      <Trans id="admin.forward-auth-apps.field.client-id">
                        Client ID
                      </Trans>
                    }
                    description={
                      <Trans id="admin.forward-auth-apps.field.client-id.hint">
                        Identifies this application. It cannot be changed later.
                      </Trans>
                    }
                    autoComplete="off"
                    spellCheck={false}
                    isMonospace
                    compactValue
                  />
                )}
              </form.AppField>
            </div>
          </Section>

          <Section
            id="service"
            icon={<GlobeIcon />}
            title={<Trans id="admin.forward-auth-apps.service">Service</Trans>}
            description={
              <Trans id="admin.forward-auth-apps.new.service.description">
                The host protected by the gateway.
              </Trans>
            }
          >
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
          </Section>

          <Section
            id="access"
            icon={<ShieldCheckIcon />}
            title={<Trans id="app.access.title">Access</Trans>}
            description={
              <Trans id="admin.oidc-apps.new.access.description">
                Open the application to every account or limit it to user
                groups. Groups are chosen on the application's page after it is
                created.
              </Trans>
            }
          >
            <form.AppField name="accessRestricted">
              {(field) => (
                <field.SwitchField
                  label={
                    <Trans id="admin.forward-auth-apps.field.restricted">
                      Restrict access
                    </Trans>
                  }
                  description={
                    <Trans id="admin.forward-auth-apps.field.restricted.hint">
                      Nobody can use the application until you select a user
                      group.
                    </Trans>
                  }
                />
              )}
            </form.AppField>
          </Section>

          <div className="flex flex-wrap items-center gap-2">
            <form.SubmitButton>
              <Trans id="admin.forward-auth-apps.new.submit">
                Create application
              </Trans>
            </form.SubmitButton>
            <CancelLink to="/admin/forward-auth-apps" pending={submitting} />
          </div>
        </form.Form>
      </form.AppForm>
    </PageBody>
  );
}
