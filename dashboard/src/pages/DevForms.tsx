import { Card } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import {
  logoutMutationOptions,
  renameCredentialMutationOptions,
} from "@/api/mutations";
import { PageHeader } from "@/components/custom/PreviewLayout";
import { SurfaceAlert } from "@/components/custom/SurfaceAlert";
import { applyServerError, type ServerFieldMap } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

const nicknameFields = {
  locations: { "body.id": "id", "body.nickname": "nickname" },
  codes: { invalid_nickname: "nickname" },
} satisfies ServerFieldMap<"id" | "nickname">;

function RenameForm() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const mutation = useMutation(renameCredentialMutationOptions(queryClient));
  const [saved, setSaved] = useState(false);
  const form = useAppForm({
    defaultValues: { id: "", nickname: "" },
    onSubmit: async ({ value, formApi }) => {
      setSaved(false);
      try {
        await mutation.mutateAsync({
          id: Number(value.id),
          nickname: value.nickname,
        });
        setSaved(true);
      } catch (error) {
        if (!isCancellation(error))
          applyServerError(formApi, error, nicknameFields);
      }
    },
  });
  return (
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>
          <Trans id="dev.forms.rename.title">Credential nickname request</Trans>
        </Card.Title>
      </Card.Header>
      <Card.Content>
        <form.AppForm>
          <form.Form
            label={t({
              id: "dev.forms.rename.title",
              message: "Credential nickname request",
            })}
          >
            <form.FormError />
            <form.AppField
              name="id"
              validators={{
                onBlur: ({ value }) =>
                  /^\d+$/.test(value) &&
                  Number(value) > 0 &&
                  Number(value) <= 2147483647
                    ? undefined
                    : msg({
                        id: "dev.forms.id.invalid",
                        message:
                          "Enter a positive credential ID (up to 2147483647).",
                      }),
              }}
            >
              {(field) => (
                <field.FormField
                  label={<Trans id="dev.forms.id.label">Credential ID</Trans>}
                  inputMode="numeric"
                  description={
                    <Trans id="dev.forms.id.description">
                      Use a credential belonging to an isolated test account.
                    </Trans>
                  }
                  variant="secondary"
                />
              )}
            </form.AppField>
            <form.AppField name="nickname">
              {(field) => (
                <field.FormField
                  label={<Trans id="dev.forms.nickname.label">Nickname</Trans>}
                  description={
                    <Trans id="dev.forms.nickname.description">
                      The server validates this value. A successful request
                      changes the test credential.
                    </Trans>
                  }
                  variant="secondary"
                />
              )}
            </form.AppField>
            <form.SubmitButton>
              <Trans id="dev.forms.rename.submit">Submit nickname</Trans>
            </form.SubmitButton>
            {saved && (
              <SurfaceAlert status="success" role="status">
                <SurfaceAlert.Indicator />
                <SurfaceAlert.Content>
                  <SurfaceAlert.Title>
                    <Trans id="dev.forms.rename.success">
                      Nickname saved. Your input has been kept.
                    </Trans>
                  </SurfaceAlert.Title>
                </SurfaceAlert.Content>
              </SurfaceAlert>
            )}
          </form.Form>
        </form.AppForm>
      </Card.Content>
    </Card>
  );
}

function LogoutForm() {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const mutation = useMutation(logoutMutationOptions(queryClient));
  const [completed, setCompleted] = useState(false);
  const form = useAppForm({
    defaultValues: {},
    onSubmit: async ({ formApi }) => {
      setCompleted(false);
      try {
        await mutation.mutateAsync();
        setCompleted(true);
      } catch (error) {
        if (!isCancellation(error))
          applyServerError(formApi, error, { locations: {}, codes: {} });
      }
    },
  });
  return (
    <Card>
      <Card.Header>
        <Card.Title render={(props) => <h2 {...props} />}>
          <Trans id="dev.forms.logout.title">Empty response request</Trans>
        </Card.Title>
        <Card.Description>
          <Trans id="dev.forms.logout.description">
            This sends a real logout request and clears session queries. Use
            only an isolated browser session, never your regular signed-in
            session.
          </Trans>
        </Card.Description>
      </Card.Header>
      <Card.Content>
        <form.AppForm>
          <form.Form
            label={t({
              id: "dev.forms.logout.title",
              message: "Empty response request",
            })}
          >
            <form.FormError />
            <form.SubmitButton>
              <Trans id="dev.forms.logout.submit">Log out test session</Trans>
            </form.SubmitButton>
            {completed && (
              <SurfaceAlert status="success" role="status">
                <SurfaceAlert.Indicator />
                <SurfaceAlert.Content>
                  <SurfaceAlert.Title>
                    <Trans id="dev.forms.logout.success">
                      Logout completed with no response body.
                    </Trans>
                  </SurfaceAlert.Title>
                </SurfaceAlert.Content>
              </SurfaceAlert>
            )}
          </form.Form>
        </form.AppForm>
      </Card.Content>
    </Card>
  );
}

export default function DevForms() {
  return (
    <>
      <PageHeader
        eyebrow={<Trans id="dev.forms.eyebrow">Development only</Trans>}
        title={<Trans id="dev.forms.title">Form integration checks</Trans>}
        description={
          <Trans id="dev.forms.description">
            Exercise the real API client, mutations, and form feedback. Use an
            isolated test account or intercept requests in browser tools. This
            page does not simulate responses.
          </Trans>
        }
      />
      <SurfaceAlert status="warning">
        <SurfaceAlert.Indicator />
        <SurfaceAlert.Content>
          <SurfaceAlert.Title>
            <Trans id="dev.forms.warning">
              These controls send real requests. They are development checks,
              not a credential management or sign-in page.
            </Trans>
          </SurfaceAlert.Title>
        </SurfaceAlert.Content>
      </SurfaceAlert>
      <RenameForm />
      <LogoutForm />
    </>
  );
}
