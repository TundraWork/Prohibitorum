import { Avatar } from "@heroui/react";
import { msg } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { ImageIcon } from "lucide-react";
import { isCancellation } from "@/api/errors";
import {
  type InstanceImageKind,
  removeInstanceImageMutationOptions,
  updateInstanceNameMutationOptions,
  uploadInstanceImageMutationOptions,
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

/** What `imageutil.ValidateRaw` accepts for the background; no GIF. */
const backgroundTypes = ["image/png", "image/jpeg", "image/webp"];

/**
 * What the instance is called and how it looks, one section per value.
 *
 * Every value here comes from `/config`, the same read the sidebar, the header,
 * the title and the sign-in page draw from, so a saved change shows everywhere
 * once it refreshes. Three separate sections rather than three cards in one:
 * each names the thing it changes, and a reader looking for the sign-in
 * background is not asked to read past the name and the icon first.
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
      <Section
        title={
          <Trans id="settings.general.background.title">
            Sign-in background
          </Trans>
        }
      >
        <BackgroundCard backgroundUrl={branding.backgroundUrl} />
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

/** The upload and removal of one instance image, with the failure to show. */
function useInstanceImage(kind: InstanceImageKind) {
  const queryClient = useQueryClient();
  const upload = useMutation(
    uploadInstanceImageMutationOptions(queryClient, kind),
  );
  const remove = useMutation(
    removeInstanceImageMutationOptions(queryClient, kind),
  );
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
  const { upload, remove, failure } = useInstanceImage("icon");

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

function BackgroundCard({ backgroundUrl }: { backgroundUrl?: string }) {
  const { upload, remove, failure } = useInstanceImage("background");

  return (
    <ConsoleCard>
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:gap-6">
        <div className="grid aspect-video w-full max-w-xs shrink-0 place-items-center overflow-hidden rounded-[0.375rem] bg-surface-secondary text-muted">
          {backgroundUrl === undefined ? (
            <ImageIcon size={24} strokeWidth={1.5} aria-hidden="true" />
          ) : (
            <img
              src={backgroundUrl}
              alt=""
              className="size-full object-cover"
            />
          )}
        </div>
        <ImageUploadControl
          types={backgroundTypes}
          hasImage={backgroundUrl !== undefined}
          uploadLabel={
            <Trans id="settings.general.background.upload">
              Upload a background
            </Trans>
          }
          replaceLabel={
            <Trans id="settings.general.background.replace">
              Replace background
            </Trans>
          }
          removeLabel={
            <Trans id="settings.general.background.remove">
              Remove background
            </Trans>
          }
          hint={
            <Trans id="settings.general.background.hint">
              PNG, JPEG or WebP, up to 5 MiB.
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
