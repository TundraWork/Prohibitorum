import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { updateMaintenanceMutationOptions } from "@/api/mutations";
import { publicConfigQueryOptions } from "@/api/queries";
import type { MaintenanceSettings } from "@/api/raw-admin-paths";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

/** `PUT /admin/settings/maintenance` refuses a longer notice. */
const maxNoticeLength = 500;

const noticeTooLong = msg({
  id: "settings.maintenance.notice.too_long",
  message: "Use 500 characters or fewer.",
});

/**
 * Maintenance mode and the notice shown while it is on.
 *
 * Turning it on locks every non-admin out until it is turned off again, so that
 * one change waits for a confirmation; turning it off, or editing the notice of
 * a mode that is already on, does not.
 */
export function MaintenancePanel() {
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  return (
    // Keyed by the saved values, so the form starts again from what the server
    // now reports once a save has gone through.
    <MaintenanceCard
      key={`${config.maintenanceMode}:${config.maintenanceMessage}`}
      saved={{
        maintenanceMode: config.maintenanceMode,
        maintenanceMessage: config.maintenanceMessage,
      }}
    />
  );
}

function MaintenanceCard({ saved }: { saved: MaintenanceSettings }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateMaintenanceMutationOptions(queryClient));
  const [confirming, setConfirming] = useState<MaintenanceSettings | null>(
    null,
  );

  const save = async (body: MaintenanceSettings) => {
    try {
      await update.mutateAsync(body);
    } catch (error) {
      if (isCancellation(error)) return;
      applyServerError(form, error, {
        locations: {},
        codes: { bad_request: "maintenanceMessage" },
      });
    }
  };

  const form = useAppForm({
    defaultValues: saved,
    onSubmit: async ({ value }) => {
      if (value.maintenanceMode && !saved.maintenanceMode) {
        setConfirming(value);
        return;
      }
      await save(value);
    },
  });

  return (
    <ConsoleCard
      title={<Trans id="settings.maintenance.title">Maintenance mode</Trans>}
    >
      <form.AppForm>
        <form.Form
          label={t({
            id: "settings.maintenance.form",
            message: "Maintenance mode",
          })}
        >
          <form.FormError />
          <form.AppField name="maintenanceMode">
            {(field) => (
              <field.SwitchField
                label={
                  <Trans id="settings.maintenance.switch">
                    Maintenance mode
                  </Trans>
                }
              />
            )}
          </form.AppField>
          <form.AppField
            name="maintenanceMessage"
            validators={{
              onChange: ({ value }) =>
                [...value].length > maxNoticeLength ? noticeTooLong : undefined,
            }}
          >
            {(field) => (
              <field.TextAreaField
                label={<Trans id="settings.maintenance.notice">Notice</Trans>}
                description={
                  <Trans id="settings.maintenance.notice.hint">
                    Shown on the sign-in page while maintenance mode is on.
                  </Trans>
                }
                rows={4}
                variant="secondary"
              />
            )}
          </form.AppField>
          <form.SubmitButton>
            <Trans id="settings.maintenance.save">Save</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>

      <ConfirmDialog
        isOpen={confirming !== null}
        onOpenChange={(open) => !open && setConfirming(null)}
        status="warning"
        title={
          <Trans id="settings.maintenance.confirm.title">
            Turn on maintenance mode?
          </Trans>
        }
        body={
          <p>
            <Trans id="settings.maintenance.confirm.body">
              Nobody except administrators can sign in or use applications until
              it is turned off.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="settings.maintenance.confirm.action">Turn on</Trans>
        }
        isPending={update.isPending}
        onConfirm={() => {
          if (!confirming) return;
          void save(confirming).finally(() => setConfirming(null));
        }}
      />
    </ConsoleCard>
  );
}
