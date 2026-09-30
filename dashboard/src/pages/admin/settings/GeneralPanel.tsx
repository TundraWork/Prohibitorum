import { Avatar } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { isCancellation } from "@/api/errors";
import {
  removeInstanceIconMutationOptions,
  updateInstanceNameMutationOptions,
  uploadInstanceIconMutationOptions,
} from "@/api/mutations";
import { publicConfigQueryOptions } from "@/api/queries";
import { ConsoleCard } from "@/components/custom/ConsoleCard";
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
 * What the instance is called and how it looks, one section per value.
 *
 * Every value here comes from `/config`, the same read the sidebar, the header,
 * the title and the sign-in page draw from, so a saved change shows everywhere
 * once it refreshes. Separate sections rather than cards in one: each names the
 * thing it changes. The sign-in page has its own panel, `SignInPagePanel`.
 */
export function GeneralPanel() {
  const { data: config } = useSuspenseQuery(publicConfigQueryOptions());
  const branding = instanceBranding(config);

  return (
    <>
      {/* Keyed by the saved name, so the field starts again from what the
          server now reports — including the configured name after the
          override was cleared. */}
      <Section
        title={<Trans id="settings.general.name.title">Instance name</Trans>}
      >
        <InstanceNameCard
          key={config.instanceName}
          name={config.instanceName}
        />
      </Section>
      <Section title={<Trans id="settings.general.icon.title">Icon</Trans>}>
        <IconCard
          iconUrl={branding.iconUrl}
          name={branding.name}
          hasCustomIcon={config.hasCustomIcon}
        />
      </Section>
    </>
  );
}

function InstanceNameCard({ name }: { name: string }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const update = useMutation(updateInstanceNameMutationOptions(queryClient));

  const form = useAppForm({
    defaultValues: { instanceName: name },
    onSubmit: async ({ value }) => {
      try {
        await update.mutateAsync(value.instanceName);
      } catch (error) {
        if (isCancellation(error)) return;
        applyServerError(form, error, {
          locations: {},
          codes: { bad_request: "instanceName" },
        });
      }
    },
  });

  return (
    <ConsoleCard>
      <form.AppForm>
        <form.Form
          label={t({
            id: "settings.general.name.form",
            message: "Instance name",
          })}
        >
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
                variant="secondary"
              />
            )}
          </form.AppField>
          <form.SubmitButton>
            <Trans id="settings.general.save">Save</Trans>
          </form.SubmitButton>
        </form.Form>
      </form.AppForm>
    </ConsoleCard>
  );
}

/** The icon's upload and removal, with the failure to show. */
function useInstanceIcon() {
  const queryClient = useQueryClient();
  const upload = useMutation(uploadInstanceIconMutationOptions(queryClient));
  const remove = useMutation(removeInstanceIconMutationOptions(queryClient));
  // Only the latest attempt's failure is worth reporting.
  const failure =
    upload.submittedAt >= remove.submittedAt ? upload.error : remove.error;
  return { upload, remove, failure };
}

function IconCard({
  iconUrl,
  name,
  hasCustomIcon,
}: {
  iconUrl: string;
  name: string;
  hasCustomIcon: boolean;
}) {
  const { upload, remove, failure } = useInstanceIcon();

  return (
    <ConsoleCard>
      <div className="flex items-center gap-6">
        <Avatar className="size-16 shrink-0">
          <Avatar.Image src={iconUrl} alt="" />
          <Avatar.Fallback>{name.slice(0, 1)}</Avatar.Fallback>
        </Avatar>
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
    </ConsoleCard>
  );
}
