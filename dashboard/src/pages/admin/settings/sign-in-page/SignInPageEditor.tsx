import { plural } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { Link, useBlocker } from "@tanstack/react-router";
import {
  ArrowLeftIcon,
  MonitorIcon,
  MoonIcon,
  SmartphoneIcon,
  SunIcon,
} from "lucide-react";
import { type ReactNode, useState } from "react";
import { ApiError, isCancellation } from "@/api/errors";
import { updateLoginAppearanceMutationOptions } from "@/api/mutations";
import { loginAppearanceQueryOptions } from "@/api/queries";
import type { AdminLoginAppearance } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
import { Button } from "@/components/custom/Button";
import { ConsequenceFooter } from "@/components/custom/ConsequenceFooter";
import { useConsoleCrumb } from "@/components/custom/console-crumb";
import { useInstanceBranding } from "@/components/custom/instance-branding";
import { PageHeader } from "@/components/custom/PageHeader";
import { Section, useSectionPanel } from "@/components/custom/Section";
import { SectionRegistryProvider } from "@/components/custom/SectionRegistry";
import { SegmentedControl } from "@/components/custom/SegmentedControl";
import {
  UnsavedBar,
  unsavedReserveClass,
  useUnsavedReserve,
} from "@/components/custom/UnsavedBar";
import { applyServerError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { BackgroundControls } from "@/pages/admin/settings/sign-in-page/BackgroundControls";
import {
  type PreviewSize,
  SignInPreview,
  usePreviewTheme,
} from "@/pages/admin/settings/sign-in-page/SignInPreview";
import {
  PlacementControls,
  SurfaceFields,
} from "@/pages/admin/settings/sign-in-page/SurfaceControls";
import {
  changedSettings,
  queryError,
  type SignInPageValues,
  signInPageBody,
} from "@/pages/admin/settings/sign-in-page/sign-in-page-form";

/** The server errors that belong to the access key field. */
const keyErrorCodes = ["unsplash_key_invalid", "unsplash_key_required"];

const isKeyRefusal = (error: unknown) =>
  error instanceof ApiError && keyErrorCodes.includes(error.code ?? "");

/**
 * How the sign-in page looks: its background, where its card sits, its theme
 * and the surfaces of its card and toolbar, with the page itself previewed
 * beside the controls.
 *
 * One Save sends the background source and its settings, the card's position,
 * the theme and the surfaces, and a new Unsplash key if one was typed. Adding
 * or removing an image and removing the key happen at once, without waiting
 * for Save.
 */
export function SignInPageEditor() {
  const { t } = useLingui();
  const { data: saved } = useSuspenseQuery(loginAppearanceQueryOptions());
  useConsoleCrumb(t({ id: "settings.sign-in.title", message: "Sign-in page" }));
  return (
    <SectionRegistryProvider>
      <div className="flex flex-col gap-8">
        <div className="flex flex-col gap-3">
          <Button
            variant="ghost"
            size="sm"
            className="-ms-2.5 w-fit"
            render={<Link to="/admin/settings" />}
          >
            <ArrowLeftIcon aria-hidden="true" />
            <Trans id="settings.sign-in.back">Back to Settings</Trans>
          </Button>
          <PageHeader
            title={<Trans id="settings.sign-in.title">Sign-in page</Trans>}
            description={
              <Trans id="settings.sign-in.page.description">
                What people see before they sign in. The preview shows your
                changes as you make them.
              </Trans>
            }
          />
        </div>
        <Editor saved={saved} />
      </div>
      {/* Below `lg` the preview, and with it Save, scrolls out of sight; the
          bar says what is unsaved and leads back to it. */}
      <UnsavedBar />
    </SectionRegistryProvider>
  );
}

function Editor({ saved }: { saved: AdminLoginAppearance }) {
  const { t } = useLingui();
  const queryClient = useQueryClient();
  const { loginImages } = useInstanceBranding();
  const update = useMutation(
    withInlineReport(
      updateLoginAppearanceMutationOptions(queryClient),
      isKeyRefusal,
    ),
  );
  const [replacingKey, setReplacingKey] = useState(false);
  const [size, setSize] = useState<PreviewSize>("desktop");
  const hasKey = saved.hasUnsplashKey;

  const form = useAppForm({
    defaultValues: {
      appearance: saved.appearance,
      unsplashAccessKey: "",
    } as SignInPageValues,
    validators: {
      // A keyword is kept while another source is chosen and its field is
      // hidden, and the server checks it all the same.
      onSubmit: ({ value }) =>
        value.appearance.background.source !== "unsplash" &&
        queryError(value.appearance.background.unsplash.query) !== undefined
          ? t({
              id: "settings.sign-in.unsplash.query.hidden_invalid",
              message:
                "The Unsplash keyword can't be used. Choose Unsplash to correct it.",
            })
          : undefined,
    },
    onSubmit: async ({ value }) => {
      // A key typed for Unsplash is sent only while Unsplash is chosen.
      const body = signInPageBody(
        value.appearance.background.source === "unsplash"
          ? value
          : { ...value, unsplashAccessKey: "" },
      );
      try {
        await update.mutateAsync(body);
        form.setFieldValue("unsplashAccessKey", "");
        setReplacingKey(false);
      } catch (error) {
        if (isCancellation(error)) return;
        // Anything but the key's refusal is the error toast's.
        if (isKeyRefusal(error)) {
          applyServerError(form, error, {
            locations: {},
            codes: {
              unsplash_key_invalid: "unsplashAccessKey",
              unsplash_key_required: "unsplashAccessKey",
            },
          });
        }
      }
    },
  });

  const appearance = useStore(form.store, (state) => state.values.appearance);
  const typedKey = useStore(
    form.store,
    (state) => state.values.unsplashAccessKey,
  );
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const changes = changedSettings(saved.appearance, appearance, typedKey);
  const preview = usePreviewTheme(appearance);
  const reserve = useUnsavedReserve();

  useBlocker({
    shouldBlockFn: () =>
      changes > 0 &&
      !window.confirm(
        t({
          id: "settings.sign-in.leave",
          message:
            "You have unsaved changes to the sign-in page. Leave without saving them?",
        }),
      ),
    enableBeforeUnload: () => changes > 0,
  });

  const discard = () => {
    form.reset({ appearance: saved.appearance, unsplashAccessKey: "" });
    setReplacingKey(false);
  };

  return (
    <div {...reserve} className={unsavedReserveClass}>
      <form.AppForm>
        <form.Form
          label={t({ id: "settings.sign-in.form", message: "Sign-in page" })}
          className="grid items-start gap-6 lg:grid-cols-[380px_minmax(0,1fr)]"
        >
          {/* The preview comes first in the page, so a narrow screen shows the
            effect above the controls; from `lg` it sits beside them and stays
            in view. */}
          <div className="flex flex-col gap-6 lg:sticky lg:top-[calc(var(--app-sticky-offset)+5rem)] lg:order-2">
            <Section
              id="preview"
              dirty={changes > 0}
              title={<Trans id="settings.sign-in.preview">Preview</Trans>}
              action={
                <PreviewToggles theme={preview} size={size} onSize={setSize} />
              }
              footer={
                changes > 0 ? (
                  <ConsequenceFooter
                    tone="neutral"
                    consequence={t({
                      id: "settings.sign-in.unsaved",
                      message: plural(changes, {
                        one: "# unsaved change",
                        other: "# unsaved changes",
                      }),
                    })}
                    actionLabel={<Trans id="settings.sign-in.save">Save</Trans>}
                    onDiscard={discard}
                    pending={submitting || update.isPending}
                  />
                ) : undefined
              }
            >
              <PanelBody>
                <SignInPreview
                  appearance={appearance}
                  images={loginImages.map((image) => image.url)}
                  hasUnsplashKey={hasKey}
                  theme={preview.theme}
                  size={size}
                />
              </PanelBody>
            </Section>
          </div>

          <div className="flex min-w-0 flex-col gap-6 lg:order-1">
            <form.FormError />
            <Section
              id="background"
              title={<Trans id="settings.sign-in.background">Background</Trans>}
            >
              <div className="flex flex-col gap-4">
                <BackgroundControls
                  form={form}
                  images={loginImages}
                  hasKey={hasKey}
                  inUse={saved.appearance.background.source === "unsplash"}
                  replacingKey={replacingKey}
                  onReplaceKey={() => setReplacingKey(true)}
                />
              </div>
            </Section>
            <Section
              id="card"
              title={<Trans id="settings.sign-in.card">Card</Trans>}
            >
              <div className="flex flex-col gap-4">
                <PlacementControls
                  position={appearance.cardPosition}
                  theme={appearance.theme}
                  disabled={submitting}
                  onPosition={(position) =>
                    form.setFieldValue("appearance.cardPosition", position)
                  }
                  onTheme={(theme) =>
                    form.setFieldValue("appearance.theme", theme)
                  }
                />
                <SurfaceFields
                  surface={appearance.card}
                  disabled={submitting}
                  onChange={(card) =>
                    form.setFieldValue("appearance.card", card)
                  }
                />
              </div>
            </Section>
            <Section
              id="toolbar"
              title={<Trans id="settings.sign-in.toolbar">Toolbar</Trans>}
            >
              <div className="flex flex-col gap-4">
                <SurfaceFields
                  surface={appearance.capsules}
                  disabled={submitting}
                  onChange={(capsules) =>
                    form.setFieldValue("appearance.capsules", capsules)
                  }
                />
              </div>
            </Section>
          </div>
        </form.Form>
      </form.AppForm>
    </div>
  );
}

/** The preview takes the whole panel, edge to edge, instead of sitting in its padding. */
function PanelBody({ children }: { children: ReactNode }) {
  useSectionPanel();
  return (
    <div className="overflow-hidden rounded-xl border bg-background shadow-xs/5">
      {children}
    </div>
  );
}

/** Which theme and which size of window the preview shows. */
function PreviewToggles({
  theme,
  size,
  onSize,
}: {
  theme: ReturnType<typeof usePreviewTheme>;
  size: PreviewSize;
  onSize: (size: PreviewSize) => void;
}) {
  const { t } = useLingui();
  const hidden = (label: string) => <span className="sr-only">{label}</span>;
  const light = t({ id: "settings.sign-in.preview.light", message: "Light" });
  const dark = t({ id: "settings.sign-in.preview.dark", message: "Dark" });
  const desktop = t({
    id: "settings.sign-in.preview.desktop",
    message: "Desktop",
  });
  const phone = t({ id: "settings.sign-in.preview.phone", message: "Phone" });
  return (
    <div className="flex flex-wrap items-center gap-2">
      {/* A draft that forces a theme settles it, so there is no choice to offer. */}
      {!theme.forced && (
        <SegmentedControl
          label={t({
            id: "settings.sign-in.preview.theme",
            message: "Preview theme",
          })}
          value={theme.theme}
          onValueChange={theme.choose}
          options={[
            {
              value: "light",
              label: (
                <>
                  <SunIcon aria-hidden="true" />
                  {hidden(light)}
                </>
              ),
            },
            {
              value: "dark",
              label: (
                <>
                  <MoonIcon aria-hidden="true" />
                  {hidden(dark)}
                </>
              ),
            },
          ]}
        />
      )}
      <SegmentedControl
        label={t({
          id: "settings.sign-in.preview.size",
          message: "Preview size",
        })}
        value={size}
        onValueChange={onSize}
        options={[
          {
            value: "desktop",
            label: (
              <>
                <MonitorIcon aria-hidden="true" />
                {hidden(desktop)}
              </>
            ),
          },
          {
            value: "phone",
            label: (
              <>
                <SmartphoneIcon aria-hidden="true" />
                {hidden(phone)}
              </>
            ),
          },
        ]}
      />
    </div>
  );
}
