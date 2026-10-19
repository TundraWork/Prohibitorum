import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { SlidersHorizontalIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import {
  removeInstanceIconMutationOptions,
  updateInstanceNameMutationOptions,
  uploadInstanceIconMutationOptions,
} from "@/api/mutations";
import { publicConfigQueryOptions } from "@/api/queries";
import { withInlineReport } from "@/app/query-client";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { EntityAvatar } from "@/components/custom/EntityAvatar";
import { ImageUploadControl } from "@/components/custom/ImageUploadControl";
import { instanceBranding } from "@/components/custom/instance-branding";
import { Section } from "@/components/custom/Section";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";

/** `PUT /admin/settings` refuses a name longer than this many characters. */
const maxNameLength = 64;

const nameTooLong = msg({
  id: "settings.general.name.too_long",
  message: "Use 64 characters or fewer.",
});

/** What the icon endpoint decodes: `image.Decode` with GIF, JPEG, PNG and WebP. */
const iconTypes = ["image/png", "image/jpeg", "image/webp", "image/gif"];

/**
 * What the instance is called and its icon.
 *
 * Both come from `/config`, the same read the sidebar, the header, the title
 * and the sign-in page draw from, so a saved change shows everywhere once it
 * refreshes. The name is saved from the section's footer; the icon is its own
 * write, made when the file is chosen, so it is not part of what the footer
 * saves. The sign-in page has its own panel.
 */
export function GeneralPanel() {
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  const branding = instanceBranding(config);

  return (
    // Keyed by the saved name, so the field starts again from what the server
    // now reports — including the configured name after the override was
    // cleared.
    <GeneralSection
      key={config.instanceName}
      name={config.instanceName}
      iconUrl={branding.iconUrl}
      hasCustomIcon={config.hasCustomIcon}
    />
  );
}

function GeneralSection({
  name,
  iconUrl,
  hasCustomIcon,
}: {
  name: string;
  iconUrl: string;
  hasCustomIcon: boolean;
}) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(
    withInlineReport(updateInstanceNameMutationOptions(queryClient), true),
  );

  const form = useAppForm({
    defaultValues: { instanceName: name },
    onSubmit: async ({ value }) => {
      try {
        await update.mutateAsync(value.instanceName);
        // The server keeps what was sent; the section reads as saved now and
        // the refreshed config re-keys it to the effective name.
        form.reset(value);
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          locations: {},
          codes: { bad_request: "instanceName" },
        });
      }
    },
  });

  const dirty = useStore(form.store, (state) => !state.isDefaultValue);
  const submitting = useStore(form.store, (state) => state.isSubmitting);

  const title = t({ id: "settings.general.title", message: "General" });

  return (
    <form.AppForm>
      <form.Form label={title}>
        <Section
          id="general"
          icon={<SlidersHorizontalIcon />}
          title={title}
          description={
            <Trans id="settings.general.description">
              The name and icon people see in the console, on the sign-in page
              and in the browser tab.
            </Trans>
          }
          dirty={dirty}
          footer={
            dirty ? (
              <ConsequenceFooter
                actionLabel={<Trans id="settings.general.save">Save</Trans>}
                onDiscard={() => form.reset()}
                pending={submitting || update.isPending}
              />
            ) : undefined
          }
        >
          <div className="flex flex-col gap-6">
            <form.FormError />

            <form.AppField
              name="instanceName"
              validators={{
                onChange: ({ value }) =>
                  [...value].length > maxNameLength ? nameTooLong : undefined,
              }}
            >
              {(field) => (
                <field.FormField
                  label={<Trans id="settings.general.name.label">Name</Trans>}
                  description={
                    <Trans id="settings.general.name.hint">
                      Leave it empty to use the name from the deployment
                      configuration.
                    </Trans>
                  }
                  autoComplete="off"
                />
              )}
            </form.AppField>

            <div className="flex flex-col gap-2">
              <div className="font-medium text-sm">
                <Trans id="settings.general.icon.title">Icon</Trans>
              </div>
              <InstanceIconControl
                iconUrl={iconUrl}
                name={name}
                hasCustomIcon={hasCustomIcon}
              />
            </div>
          </div>
        </Section>
      </form.Form>
    </form.AppForm>
  );
}

/** The icon's preview with its upload and removal, whose failure it shows. */
function InstanceIconControl({
  iconUrl,
  name,
  hasCustomIcon,
}: {
  iconUrl: string;
  name: string;
  hasCustomIcon: boolean;
}) {
  const queryClient = useQueryClient();
  const upload = useMutation(
    withInlineReport(uploadInstanceIconMutationOptions(queryClient), true),
  );
  const remove = useMutation(
    withInlineReport(removeInstanceIconMutationOptions(queryClient), true),
  );
  // Only the latest attempt's failure is worth reporting.
  const failure =
    upload.submittedAt >= remove.submittedAt ? upload.error : remove.error;

  return (
    <div className="flex items-center gap-6">
      <EntityAvatar
        iconUrl={iconUrl}
        className="size-16 shrink-0"
        name={name}
        fallbackClassName="text-lg"
      />
      <ImageUploadControl
        types={iconTypes}
        hasImage={hasCustomIcon}
        uploadLabel={
          <Trans id="settings.general.icon.upload">Upload an icon</Trans>
        }
        replaceLabel={
          <Trans id="settings.general.icon.replace">Replace icon</Trans>
        }
        removeLabel={
          <Trans id="settings.general.icon.remove">Remove icon</Trans>
        }
        hint={
          <Trans id="settings.general.icon.hint">
            PNG, JPEG, WebP or GIF, up to 5 MiB.
          </Trans>
        }
        isUploading={upload.isPending}
        isRemoving={remove.isPending}
        failure={failure}
        onUpload={(file) => upload.mutate(file)}
        onRemove={() => remove.mutate()}
      />
    </div>
  );
}
