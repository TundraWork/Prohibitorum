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
import { withInlineReport } from "@/app/query-client";
import { ConfirmDialog } from "@/components/custom/ConfirmDialog";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { Section } from "@/components/custom/Section";
import { Badge } from "@/components/ui/badge";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import type { Locale } from "@/i18n";
import { localeNames, locales, localeTags, resolveLocale } from "@/i18n";

/** `PUT /admin/settings/maintenance` refuses a longer notice. */
const maxNoticeLength = 500;

const noticeTooLong = msg({
  id: "settings.maintenance.notice.too_long",
  message: "Use 500 characters or fewer.",
});

const defaultNoticeRequired = msg({
  id: "settings.maintenance.notice.default_required",
  message:
    "Write the notice in the default language too, so every visitor sees it.",
});

/** The form's value: one text box per language, an empty string meaning none. */
type MaintenanceFormValue = {
  maintenanceMode: boolean;
  maintenanceDefaultLocale: Locale;
  maintenanceMessages: Record<Locale, string>;
};

/** Every language present, missing ones read as no notice. */
function messagesFromConfig(config: {
  maintenanceMessages: Partial<Record<string, string>>;
}): Record<Locale, string> {
  return Object.fromEntries(
    locales.map((locale) => [locale, config.maintenanceMessages[locale] ?? ""]),
  ) as Record<Locale, string>;
}

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
 * Maintenance mode and the multilingual notice shown while it is on.
 *
 * The notice is one text per UI language; the default language is what a
 * visitor sees when their own language has no notice, so it must be filled in
 * whenever any language is — the form and the server both refuse otherwise.
 *
 * Turning maintenance on locks every non-admin out until it is turned off
 * again, so that one change states its consequence in the footer and waits for
 * a confirmation when the form is saved; turning it off, or editing the notice
 * of a mode that is already on, does not.
 */
export function MaintenancePanel() {
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  const saved: MaintenanceFormValue = {
    maintenanceMode: config.maintenanceMode,
    maintenanceDefaultLocale: resolveLocale(config.maintenanceDefaultLocale),
    maintenanceMessages: messagesFromConfig(config),
  };
  return (
    // Keyed by the saved values, so the form starts again from what the server
    // now reports once a save has gone through.
    <MaintenanceSection
      key={`${config.maintenanceMode}:${config.maintenanceDefaultLocale}:${JSON.stringify(config.maintenanceMessages)}`}
      saved={saved}
    />
  );
}

function MaintenanceSection({ saved }: { saved: MaintenanceFormValue }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateMaintenanceMutationOptions(queryClient), true),
  );
  // The checked values the dialog is confirming, held beside the form.
  const [confirming, setConfirming] = useState<MaintenanceFormValue | null>(
    null,
  );

  const save = async (value: MaintenanceFormValue) => {
    try {
      await update.mutateAsync(value);
      form.reset(value);
    } catch (error) {
      if (isCancellation(error)) return;
      // A refused PUT lands on the default language's box: the refusals name
      // no field, and the default language is the one whose notice both error
      // codes are about. (An over-long notice never reaches the PUT — the
      // onChange limit check holds the form back — so no per-language guess.)
      applyServerError(form, error, {
        locations: {},
        codes: {
          bad_request: `maintenanceMessages.${value.maintenanceDefaultLocale}`,
          maintenance_default_notice_required: `maintenanceMessages.${value.maintenanceDefaultLocale}`,
        },
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
  const defaultLocale = useStore(
    form.store,
    (state) => state.values.maintenanceDefaultLocale,
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
              <form.AppField name="maintenanceDefaultLocale">
                {(field) => (
                  <field.SegmentedField
                    label={t({
                      id: "settings.maintenance.default_locale",
                      message: "Default language",
                    })}
                    description={
                      <Trans id="settings.maintenance.default_locale.hint">
                        Shown when a visitor's language has no notice.
                      </Trans>
                    }
                    options={locales.map((locale) => ({
                      value: locale,
                      label: (
                        <span lang={localeTags[locale]}>
                          {localeNames[locale]}
                        </span>
                      ),
                    }))}
                  />
                )}
              </form.AppField>
              {locales.map((locale) => (
                <form.AppField
                  key={locale}
                  name={`maintenanceMessages.${locale}`}
                  validators={{
                    onChange: ({ value }) =>
                      [...value].length > maxNoticeLength
                        ? noticeTooLong
                        : undefined,
                    onSubmit: ({ value, fieldApi }) => {
                      // The over-long case is left to the onChange validator;
                      // reaching the server with one is reported by the
                      // server-error mapping in save().
                      const messages = fieldApi.form.getFieldValue(
                        "maintenanceMessages",
                      ) as Record<Locale, string>;
                      const currentDefault = fieldApi.form.getFieldValue(
                        "maintenanceDefaultLocale",
                      ) as Locale;
                      const anyNotice = locales.some((l) => messages[l] !== "");
                      if (
                        anyNotice &&
                        locale === currentDefault &&
                        value === ""
                      ) {
                        return defaultNoticeRequired;
                      }
                      return undefined;
                    },
                  }}
                >
                  {(field) => (
                    <field.TextAreaField
                      label={
                        <span className="flex items-center gap-2">
                          <span lang={localeTags[locale]}>
                            {localeNames[locale]}
                          </span>
                          {locale === defaultLocale && (
                            <Badge>
                              <Trans id="settings.maintenance.default_badge">
                                Default
                              </Trans>
                            </Badge>
                          )}
                        </span>
                      }
                      description={
                        locale === defaultLocale ? (
                          <Trans id="settings.maintenance.notice.hint">
                            Shown on the sign-in page and the maintenance page
                            while maintenance mode is on. A visitor sees the
                            notice in their own language, or this one when
                            theirs has none.
                          </Trans>
                        ) : undefined
                      }
                      rows={4}
                    />
                  )}
                </form.AppField>
              ))}
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
