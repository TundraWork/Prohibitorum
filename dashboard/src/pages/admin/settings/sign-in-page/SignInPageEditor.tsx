import { msg, plural } from "@lingui/core/macro";
import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  useMutation,
  useQueryClient,
  useSuspenseQuery,
} from "@tanstack/react-query";
import { useBlocker } from "@tanstack/react-router";
import {
  EyeIcon,
  ImageIcon,
  MonitorIcon,
  MoonIcon,
  PanelTopIcon,
  SmartphoneIcon,
  SquareUserIcon,
  SunIcon,
} from "lucide-react";
import { type ReactNode, useRef, useState } from "react";
import { ApiError, isCancellation } from "@/api/errors";
import { updateLoginAppearanceMutationOptions } from "@/api/mutations";
import { loginAppearanceQueryOptions } from "@/api/queries";
import type { AdminLoginAppearance } from "@/api/raw-admin-paths";
import { withInlineReport } from "@/app/query-client";
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
import { applyServerError, clearServerErrors } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { BackgroundControls } from "@/pages/admin/settings/sign-in-page/BackgroundControls";
import {
  ReadabilityLine,
  useReadability,
} from "@/pages/admin/settings/sign-in-page/ReadabilityLine";
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
  changesBySection,
  colorError,
  queryError,
  type SignInPageValues,
  signInPageBody,
} from "@/pages/admin/settings/sign-in-page/sign-in-page-form";

/** The server errors that belong to a key field, by the source they name. */
const keyErrorCodes = [
  "unsplash_key_invalid",
  "unsplash_key_required",
  "pixabay_key_invalid",
  "pixabay_key_required",
] as const;

const isKeyRefusal = (error: unknown) =>
  error instanceof ApiError &&
  (keyErrorCodes as readonly string[]).includes(error.code ?? "");

/** A keyword kept for a hidden source would be rejected by the server. */
const hiddenKeywordInvalid = msg({
  id: "settings.sign-in.query.hidden_invalid",
  message: "The {source} keyword can't be used. Choose {source} to correct it.",
});

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
        <PageHeader
          title={<Trans id="settings.sign-in.title">Sign-in page</Trans>}
          description={
            <Trans id="settings.sign-in.page.description">
              What people see before they sign in. The preview shows your
              changes as you make them.
            </Trans>
          }
        />
        <Editor saved={saved} />
      </div>
      {/* Below `lg` the preview, and with it Save, scrolls out of sight; the
          bar says something is unsaved and leads back to Save. */}
      <UnsavedBar saveIn="preview" />
    </SectionRegistryProvider>
  );
}

function Editor({ saved }: { saved: AdminLoginAppearance }) {
  const { t, i18n } = useLingui();
  const queryClient = useQueryClient();
  const { loginImages } = useInstanceBranding();
  const update = useMutation(
    withInlineReport(
      updateLoginAppearanceMutationOptions(queryClient),
      isKeyRefusal,
    ),
  );
  const [replacingKey, setReplacingKey] = useState<
    "unsplash" | "pixabay" | undefined
  >();
  // A phone shows the page at its own width; a window that size could not read
  // a desktop page scaled down into it.
  const [size, setSize] = useState<PreviewSize>(() =>
    typeof window.matchMedia === "function" &&
    window.matchMedia("(max-width: 639px)").matches
      ? "phone"
      : "desktop",
  );
  const [previewDocument, setPreviewDocument] = useState<Document | null>(null);
  const hasUnsplashKey = saved.hasUnsplashKey;
  const hasPixabayKey = saved.hasPixabayKey;

  const form = useAppForm({
    defaultValues: {
      appearance: saved.appearance,
      unsplashAccessKey: "",
      pixabayApiKey: "",
    } as SignInPageValues,
    validators: {
      // A keyword is kept while another source is chosen and its field is
      // hidden, and the server checks it all the same.
      onSubmit: ({ value }) => {
        const hidden: string[] = [];
        if (
          value.appearance.background.source !== "unsplash" &&
          queryError(value.appearance.background.unsplash.query) !== undefined
        ) {
          hidden.push("Unsplash");
        }
        if (
          value.appearance.background.source !== "pixabay" &&
          queryError(value.appearance.background.pixabay.query) !== undefined
        ) {
          hidden.push("Pixabay");
        }
        return hidden.length > 0
          ? i18n._({
              ...hiddenKeywordInvalid,
              values: { source: hidden.join(" and ") },
            })
          : undefined;
      },
    },
    onSubmit: async ({ value }) => {
      // A typed key is sent only while its service is chosen.
      const body = signInPageBody(value);
      try {
        await update.mutateAsync(body);
        // What was saved is the form's new starting point.
        form.reset({
          appearance: body.appearance,
          unsplashAccessKey: "",
          pixabayApiKey: "",
        });
        setReplacingKey(undefined);
      } catch (error) {
        if (isCancellation(error)) return;
        // Anything but a key's refusal is the error toast's.
        if (isKeyRefusal(error)) {
          applyServerError(form, error, {
            locations: {},
            codes: {
              unsplash_key_invalid: "unsplashAccessKey",
              unsplash_key_required: "unsplashAccessKey",
              pixabay_key_invalid: "pixabayApiKey",
              pixabay_key_required: "pixabayApiKey",
            },
          });
        }
      }
    },
  });

  const appearance = useStore(form.store, (state) => state.values.appearance);
  const typedUnsplashKey = useStore(
    form.store,
    (state) => state.values.unsplashAccessKey,
  );
  const typedPixabayKey = useStore(
    form.store,
    (state) => state.values.pixabayApiKey,
  );
  const typedKeys = [typedUnsplashKey, typedPixabayKey];
  const submitting = useStore(form.store, (state) => state.isSubmitting);
  const changes = changedSettings(saved.appearance, appearance, typedKeys);
  const where = changesBySection(saved.appearance, appearance, typedKeys);
  // A colour half typed is not one, so the preview keeps the last that was.
  const lastColor = useRef(saved.appearance.background.color);
  if (colorError(appearance.background.color) === undefined)
    lastColor.current = appearance.background.color;
  const shown =
    colorError(appearance.background.color) === undefined
      ? appearance
      : {
          ...appearance,
          background: { ...appearance.background, color: lastColor.current },
        };
  const preview = usePreviewTheme(appearance);
  const readability = useReadability(previewDocument, shown);
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

  // The preview, and Save with it, sits beside the form rather than inside it,
  // so a page drawn in the preview never shares a form with the editor.
  const submit = () => {
    clearServerErrors(form);
    void form.handleSubmit();
  };

  const discard = () => {
    form.reset({
      appearance: saved.appearance,
      unsplashAccessKey: "",
      pixabayApiKey: "",
    });
    setReplacingKey(undefined);
  };

  return (
    <div {...reserve} className={unsavedReserveClass}>
      <form.AppForm>
        <div className="grid items-start gap-6 lg:grid-cols-[380px_minmax(0,1fr)]">
          {/* The preview comes first in the page, so a narrow screen shows the
            effect above the controls; from `lg` it sits beside them and stays
            in view. */}
          <div className="flex flex-col gap-6 lg:sticky lg:top-[calc(var(--app-sticky-offset)+5rem)] lg:order-2">
            <Section
              id="preview"
              icon={<EyeIcon />}
              title={<Trans id="settings.sign-in.preview">Preview</Trans>}
              description={
                <Trans id="settings.sign-in.preview.description">
                  The sign-in page at these settings, as visitors see it.
                </Trans>
              }
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
                    onSubmit={submit}
                    pending={submitting || update.isPending}
                  />
                ) : undefined
              }
            >
              <PanelBody>
                <SignInPreview
                  appearance={shown}
                  images={loginImages.map((image) => image.url)}
                  hasKeys={{
                    unsplash: hasUnsplashKey,
                    pixabay: hasPixabayKey,
                  }}
                  theme={preview.theme}
                  size={size}
                  onDocument={setPreviewDocument}
                />
              </PanelBody>
            </Section>
          </div>

          <form.Form
            label={t({ id: "settings.sign-in.form", message: "Sign-in page" })}
            className="flex min-w-0 flex-col gap-6 lg:order-1"
          >
            <form.FormError />
            <Section
              id="background"
              icon={<ImageIcon />}
              dirty={where.background > 0}
              action={<ChangeMark changes={where.background} />}
              title={<Trans id="settings.sign-in.background">Background</Trans>}
              description={
                <Trans id="settings.sign-in.background.description">
                  What fills the page behind the card.
                </Trans>
              }
            >
              <div className="flex flex-col gap-4">
                <BackgroundControls
                  form={form}
                  images={loginImages}
                  hasUnsplashKey={hasUnsplashKey}
                  hasPixabayKey={hasPixabayKey}
                  inUse={
                    saved.appearance.background.source === "unsplash" ||
                    saved.appearance.background.source === "pixabay"
                      ? saved.appearance.background.source
                      : undefined
                  }
                  replacingKey={replacingKey}
                  onReplaceKey={setReplacingKey}
                  onKeepKey={() => setReplacingKey(undefined)}
                />
              </div>
            </Section>
            <Section
              id="card"
              icon={<SquareUserIcon />}
              dirty={where.card > 0}
              action={<ChangeMark changes={where.card} />}
              title={<Trans id="settings.sign-in.card">Card</Trans>}
              description={
                <Trans id="settings.sign-in.card.description">
                  Where the sign-in card sits, which theme the page uses and how
                  the card looks.
                </Trans>
              }
              footer={readability && <ReadabilityLine result={readability} />}
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
                  scheme={appearance.scheme}
                  onScheme={(scheme) =>
                    form.setFieldValue("appearance.scheme", scheme)
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
              icon={<PanelTopIcon />}
              dirty={where.toolbar > 0}
              action={<ChangeMark changes={where.toolbar} />}
              title={<Trans id="settings.sign-in.toolbar">Toolbar</Trans>}
              description={
                <Trans id="settings.sign-in.toolbar.description">
                  The instance name and the language and theme buttons at the
                  top of the page.
                </Trans>
              }
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
          </form.Form>
        </div>
      </form.AppForm>
    </div>
  );
}

/** Says how many settings in a section differ from what is saved; nothing when none do. */
function ChangeMark({ changes }: { changes: number }) {
  const { t } = useLingui();
  if (changes === 0) return null;
  return (
    <span className="flex shrink-0 items-center gap-1.5 whitespace-nowrap text-muted-foreground text-xs">
      <span aria-hidden="true" className="size-1.5 rounded-full bg-warning" />
      {t({
        id: "settings.sign-in.unsaved",
        message: plural(changes, {
          one: "# unsaved change",
          other: "# unsaved changes",
        }),
      })}
    </span>
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
      {theme.forced && (
        <span className="text-muted-foreground text-xs">
          <Trans id="settings.sign-in.preview.theme_forced">
            The theme is set in Card.
          </Trans>
        </span>
      )}
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
