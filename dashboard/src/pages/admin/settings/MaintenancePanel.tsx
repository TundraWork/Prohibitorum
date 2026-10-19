import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { WrenchIcon } from "lucide-react";
import { useState } from "react";
import { isCancellation } from "@/api/errors";
import { updateMaintenanceMutationOptions } from "@/api/mutations";
import { publicConfigQueryOptions } from "@/api/queries";
import type { MaintenanceSettings } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

/** `PUT /admin/settings/maintenance` refuses a longer notice. */
const maxNoticeLength = 500;

const noticeTooLong = msg({
  id: "settings.maintenance.notice.too_long",
  message: "Use 500 characters or fewer.",
});

/** What turning maintenance mode on does, in the footer and in the dialog. */
function MaintenanceConsequence() {
  return (
    <Trans id="settings.maintenance.confirm.body">
      Nobody except administrators can sign in or use applications until it is
      turned off.
    </Trans>
  );
}

/**
 * Maintenance mode and the notice shown while it is on.
 *
 * Turning it on locks every non-admin out until it is turned off again, so that
 * one change states its consequence in the footer and waits for a confirmation
 * when the form is saved; turning it off, or editing the notice of a mode that
 * is already on, does not.
 */
export function MaintenancePanel() {
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  return (
    // Keyed by the saved values, so the form starts again from what the server
    // now reports once a save has gone through.
    <MaintenanceSection
      key={`${config.maintenanceMode}:${config.maintenanceMessage}`}
      saved={{
        maintenanceMode: config.maintenanceMode,
        maintenanceMessage: config.maintenanceMessage,
      }}
    />
  );
}

function MaintenanceSection({ saved }: { saved: MaintenanceSettings }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateMaintenanceMutationOptions(queryClient), true),
  );
  // The checked values the dialog is confirming, held beside the form.
  const [confirming, setConfirming] = useState<MaintenanceSettings | null>(
    null,
  );

  const save = async (body: MaintenanceSettings) => {
    try {
      await update.mutateAsync(body);
      form.reset(body);
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

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const turningOn = useStore(
    form.store,
    (state) => state.values.maintenanceMode && !saved.maintenanceMode,
  );

  const title = t({
    id: "settings.maintenance.title",
    message: "Maintenance mode",
  });

  return (
    <>
      <form.AppForm>
        <form.Form label={title}>
          <Section
            id="maintenance"
            icon={<WrenchIcon />}
            title={title}
            description={
              <Trans id="settings.maintenance.description">
                Keep everyone but administrators out while you work on the
                instance.
              </Trans>
            }
            dirty={dirty}
            footer={
              dirty ? (
                <ConsequenceFooter
                  consequence={
                    turningOn ? <MaintenanceConsequence /> : undefined
                  }
                  actionLabel={
                    turningOn ? (
                      <Trans id="settings.maintenance.action-label">
                        Turn on maintenance and save
                      </Trans>
                    ) : (
                      <Trans id="settings.maintenance.save">Save</Trans>
                    )
                  }
                  onDiscard={() => form.reset()}
                  pending={submitting || update.isPending}
                />
              ) : undefined
            }
          >
            <div className="flex flex-col gap-6">
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
                    [...value].length > maxNoticeLength
                      ? noticeTooLong
                      : undefined,
                }}
              >
                {(field) => (
                  <field.TextAreaField
                    label={
                      <Trans id="settings.maintenance.notice">Notice</Trans>
                    }
                    description={
                      <Trans id="settings.maintenance.notice.hint">
                        Shown on the sign-in page while maintenance mode is on.
                      </Trans>
                    }
                    rows={4}
                  />
                )}
              </form.AppField>
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
          <Trans id="settings.maintenance.confirm.title">
            Turn on maintenance mode?
          </Trans>
        }
        body={
          <p>
            <Trans id="settings.maintenance.confirm.short">
              Only administrators will be able to sign in.
            </Trans>
          </p>
        }
        confirmLabel={
          <Trans id="settings.maintenance.confirm.action">Turn on</Trans>
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
