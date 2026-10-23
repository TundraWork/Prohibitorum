import { Trans, useLingui } from "@lingui/react/macro";
import { useStore } from "@tanstack/react-form";
import {
  BlendIcon,
  CameraIcon,
  ImagesIcon,
  type LucideIcon,
  MountainIcon,
  PaintBucketIcon,
  SquareDashedIcon,
  SunriseIcon,
} from "lucide-react";
import { type ReactNode, useMemo } from "react";
import type {
  LoginBackgroundSource,
  LoginGradient,
  LoginImageOrder,
} from "@/api/raw-paths";
import { ChoiceTile } from "@/components/custom/ChoiceTile";
import { inlineLinkClass } from "@/components/custom/inline-link";
import {
  gradientBackground,
  loginGradients,
} from "@/components/custom/login-appearance/gradients";
import { pixabayLanguages } from "@/components/custom/login-appearance/photo-sources";
import { SegmentedControl } from "@/components/custom/SegmentedControl";
import { Field, FieldLabel } from "@/components/ui/field";
import { RadioGroup } from "@/components/ui/radio-group";
import {
  Select,
  SelectItem,
  SelectPopup,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Switch } from "@/components/ui/switch";
import { withForm } from "@/forms/use-app-form";
import { ChoiceStrip } from "@/pages/admin/settings/sign-in-page/ChoiceStrip";
import { ColorControl } from "@/pages/admin/settings/sign-in-page/ColorControl";
import { IntervalField } from "@/pages/admin/settings/sign-in-page/IntervalField";
import { LoginImagesControl } from "@/pages/admin/settings/sign-in-page/LoginImagesControl";
import { SavedWallpaperKey } from "@/pages/admin/settings/sign-in-page/SavedWallpaperKey";
import {
  backgroundSources,
  bingMarkets,
  colorError,
  intervalError,
  keyError,
  queryError,
  type SignInPageValues,
} from "@/pages/admin/settings/sign-in-page/sign-in-page-form";

const sourceIcons: Record<LoginBackgroundSource, LucideIcon> = {
  none: SquareDashedIcon,
  color: PaintBucketIcon,
  gradient: BlendIcon,
  bing: SunriseIcon,
  unsplash: CameraIcon,
  pixabay: MountainIcon,
  images: ImagesIcon,
};

/** "China · Chinese", in the console's language. */
function useMarketName(): (market: string) => string {
  const { i18n } = useLingui();
  const locale = i18n.locale === "zh" ? "zh-CN" : "en";
  return useMemo(() => {
    const regions = new Intl.DisplayNames([locale], { type: "region" });
    const languages = new Intl.DisplayNames([locale], { type: "language" });
    return (market: string) => {
      const [language = "", region = ""] = market.split("-");
      return `${regions.of(region) ?? region} · ${languages.of(language) ?? language}`;
    };
  }, [locale]);
}

function GradientPicker({
  value,
  disabled,
  onChange,
}: {
  value: LoginGradient;
  disabled: boolean;
  onChange: (gradient: LoginGradient) => void;
}) {
  const { t } = useLingui();
  const names: Record<LoginGradient, string> = {
    dawn: t({ id: "settings.sign-in.gradient.dawn", message: "Dawn" }),
    lagoon: t({ id: "settings.sign-in.gradient.lagoon", message: "Lagoon" }),
    aurora: t({ id: "settings.sign-in.gradient.aurora", message: "Aurora" }),
    dusk: t({ id: "settings.sign-in.gradient.dusk", message: "Dusk" }),
    mist: t({ id: "settings.sign-in.gradient.mist", message: "Mist" }),
    ember: t({ id: "settings.sign-in.gradient.ember", message: "Ember" }),
  };
  return (
    <RadioGroup
      aria-label={t({ id: "settings.sign-in.gradient", message: "Gradient" })}
      value={value}
      disabled={disabled}
      onValueChange={(next) => onChange(next as LoginGradient)}
      className="grid grid-cols-3 gap-2"
    >
      {loginGradients.map((id) => (
        <ChoiceTile
          key={id}
          value={id}
          layout="compact"
          media={
            <span
              aria-hidden="true"
              data-gradient-swatch={id}
              className="h-5 w-full rounded-sm ring-1 ring-black/10 ring-inset dark:ring-white/20"
              style={{ background: gradientBackground[id] }}
            />
          }
          label={names[id]}
        />
      ))}
    </RadioGroup>
  );
}

function MarketSelect({
  value,
  disabled,
  onChange,
}: {
  value: string;
  disabled: boolean;
  onChange: (market: string) => void;
}) {
  const marketName = useMarketName();
  return (
    <Field disabled={disabled}>
      <FieldLabel>
        <Trans id="settings.sign-in.bing.region">Region</Trans>
      </FieldLabel>
      <Select
        value={value}
        disabled={disabled}
        onValueChange={(next) => {
          if (typeof next === "string") onChange(next);
        }}
      >
        <SelectTrigger>
          <SelectValue>{(market: string) => marketName(market)}</SelectValue>
        </SelectTrigger>
        <SelectPopup alignItemWithTrigger={false}>
          {bingMarkets.map((market) => (
            <SelectItem key={market} value={market}>
              {marketName(market)}
            </SelectItem>
          ))}
        </SelectPopup>
      </Select>
    </Field>
  );
}

/** The language name in the console's language, e.g. "Chinese" for zh. */
function useLanguageName(): (code: string) => string {
  const { i18n } = useLingui();
  const locale = i18n.locale === "zh" ? "zh-CN" : "en";
  return useMemo(() => {
    const names = new Intl.DisplayNames([locale], { type: "language" });
    return (code: string) => names.of(code) ?? code;
  }, [locale]);
}

function LanguageSelect({
  value,
  disabled,
  onChange,
}: {
  value: string;
  disabled: boolean;
  onChange: (lang: string) => void;
}) {
  const languageName = useLanguageName();
  return (
    <Field disabled={disabled}>
      <FieldLabel>
        <Trans id="settings.sign-in.pixabay.language">Search language</Trans>
      </FieldLabel>
      <Select
        value={value}
        disabled={disabled}
        onValueChange={(next) => {
          if (typeof next === "string") onChange(next);
        }}
      >
        <SelectTrigger>
          <SelectValue>{(code: string) => languageName(code)}</SelectValue>
        </SelectTrigger>
        <SelectPopup alignItemWithTrigger={false}>
          {pixabayLanguages.map((code) => (
            <SelectItem key={code} value={code}>
              {languageName(code)}
            </SelectItem>
          ))}
        </SelectPopup>
      </Select>
    </Field>
  );
}

/**
 * How a source with several pictures shows them: one at random per visit, or
 * a carousel, whose interval field is passed as `children` and shown only
 * while the carousel is chosen. Uploaded images and Unsplash share it.
 */
function RotationControls({
  order,
  disabled,
  onOrder,
  children,
}: {
  order: LoginImageOrder;
  disabled: boolean;
  onOrder: (order: LoginImageOrder) => void;
  children: ReactNode;
}) {
  const { t } = useLingui();
  return (
    <>
      <ChoiceStrip
        label={t({ id: "settings.sign-in.order", message: "Order" })}
        value={order}
        disabled={disabled}
        onChange={onOrder}
        options={[
          {
            value: "random",
            label: t({
              id: "settings.sign-in.order.random",
              message: "A random one each visit",
            }),
          },
          {
            value: "carousel",
            label: t({
              id: "settings.sign-in.order.carousel",
              message: "Carousel",
            }),
          },
        ]}
      />
      {order === "carousel" && children}
    </>
  );
}

/**
 * The background source and the settings of the one chosen. Every source keeps
 * its own settings while another is chosen, and a hidden source's values are
 * still saved.
 *
 * `inUse` names the service whose background is saved, which is what holds
 * its key; `hasUnsplashKey`/`hasPixabayKey` say there is one. A key field is
 * shown while there is no key to fall back on, or while the administrator
 * chose to replace it.
 */
export const BackgroundControls = withForm({
  defaultValues: {
    appearance: {} as SignInPageValues["appearance"],
    unsplashAccessKey: "",
    pixabayApiKey: "",
  } satisfies SignInPageValues,
  props: {} as {
    images: { id: number; url: string }[];
    hasUnsplashKey: boolean;
    hasPixabayKey: boolean;
    /** The service the saved background uses, if it is a key-holding one. */
    inUse: "unsplash" | "pixabay" | undefined;
    /** The service whose key is being replaced, if any. */
    replacingKey: "unsplash" | "pixabay" | undefined;
    onReplaceKey: (source: "unsplash" | "pixabay") => void;
    onKeepKey: (source: "unsplash" | "pixabay") => void;
  },
  render: function Render({
    form,
    images,
    hasUnsplashKey,
    hasPixabayKey,
    inUse,
    replacingKey,
    onReplaceKey,
    onKeepKey,
  }) {
    const { t } = useLingui();
    const submitting = useStore(form.store, (state) => state.isSubmitting);
    const background = useStore(
      form.store,
      (state) => state.values.appearance.background,
    );
    const names: Record<LoginBackgroundSource, string> = {
      none: t({ id: "settings.sign-in.source.none", message: "Default" }),
      color: t({ id: "settings.sign-in.source.color", message: "Color" }),
      gradient: t({
        id: "settings.sign-in.source.gradient",
        message: "Gradient",
      }),
      bing: "Bing",
      unsplash: "Unsplash",
      pixabay: "Pixabay",
      images: t({ id: "settings.sign-in.source.images", message: "Images" }),
    };
    const showKeyField = !hasUnsplashKey || replacingKey === "unsplash";

    return (
      <>
        <SegmentedControl
          label={t({
            id: "settings.sign-in.source",
            message: "Background source",
          })}
          value={background.source}
          size="default"
          disabled={submitting}
          className="grid w-full grid-cols-3"
          options={backgroundSources.map((source) => {
            const Icon = sourceIcons[source];
            return {
              value: source,
              label: (
                <>
                  <Icon aria-hidden="true" />
                  {names[source]}
                </>
              ),
            };
          })}
          onValueChange={(source) =>
            form.setFieldValue("appearance.background.source", source)
          }
        />

        {background.source === "none" && (
          <p className="text-muted-foreground text-sm">
            <Trans id="settings.sign-in.none.hint">
              The page's own background, which follows light and dark mode.
            </Trans>
          </p>
        )}

        {background.source === "color" && (
          <form.Field
            name="appearance.background.color"
            validators={{ onSubmit: ({ value }) => colorError(value) }}
          >
            {(field) => (
              <ColorControl
                value={field.state.value}
                errors={field.state.meta.errors.flat(Number.POSITIVE_INFINITY)}
                disabled={submitting}
                onChange={(hex) => field.handleChange(hex)}
              />
            )}
          </form.Field>
        )}

        {background.source === "gradient" && (
          <form.Field name="appearance.background.gradient">
            {(field) => (
              <GradientPicker
                value={field.state.value}
                disabled={submitting}
                onChange={(gradient) => field.handleChange(gradient)}
              />
            )}
          </form.Field>
        )}

        {background.source === "bing" && (
          <>
            <form.Field name="appearance.background.bing.market">
              {(field) => (
                <MarketSelect
                  value={field.state.value}
                  disabled={submitting}
                  onChange={(market) => field.handleChange(market)}
                />
              )}
            </form.Field>
            <form.Field name="appearance.background.bing.showCaption">
              {(field) => (
                <Field disabled={submitting}>
                  <div className="flex w-full items-center justify-between gap-4">
                    <FieldLabel>
                      <Trans id="settings.sign-in.bing.caption">
                        Show the picture's title and copyright
                      </Trans>
                    </FieldLabel>
                    <Switch
                      checked={field.state.value}
                      onCheckedChange={(next) => field.handleChange(next)}
                    />
                  </div>
                </Field>
              )}
            </form.Field>
          </>
        )}

        {background.source === "unsplash" && (
          <>
            {hasUnsplashKey && (
              <SavedWallpaperKey
                source="unsplash"
                inUse={inUse === "unsplash"}
                disabled={submitting}
                replacing={replacingKey === "unsplash"}
                onReplace={() => onReplaceKey("unsplash")}
                onKeep={() => {
                  form.setFieldValue("unsplashAccessKey", "");
                  onKeepKey("unsplash");
                }}
              />
            )}
            {showKeyField && (
              <form.AppField
                name="unsplashAccessKey"
                validators={{
                  onSubmit: ({ value, fieldApi }) =>
                    keyError(value, {
                      source:
                        fieldApi.form.getFieldValue("appearance").background
                          .source,
                      service: "unsplash",
                      hasSavedKey: hasUnsplashKey,
                    }),
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      replacingKey ? (
                        <Trans id="settings.sign-in.unsplash.key.new">
                          New access key
                        </Trans>
                      ) : (
                        <Trans id="settings.sign-in.unsplash.key">
                          Access key
                        </Trans>
                      )
                    }
                    type="password"
                    autoComplete="off"
                    spellCheck={false}
                    isMonospace
                    compactValue
                    description={
                      <Trans id="settings.sign-in.unsplash.key.hint">
                        From your application on{" "}
                        <a
                          href="https://unsplash.com/developers"
                          target="_blank"
                          rel="noreferrer"
                          className={inlineLinkClass}
                        >
                          unsplash.com/developers
                        </a>
                        .
                      </Trans>
                    }
                  />
                )}
              </form.AppField>
            )}
            <form.AppField
              name="appearance.background.unsplash.query"
              validators={{
                onBlur: ({ value }) => queryError(value),
                onSubmit: ({ value }) => queryError(value),
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="settings.sign-in.unsplash.query">Keyword</Trans>
                  }
                  placeholder="mountains"
                  autoComplete="off"
                  description={
                    <Trans id="settings.sign-in.unsplash.query.hint">
                      Leave it empty for any photo.
                    </Trans>
                  }
                />
              )}
            </form.AppField>
            {/* Always offered: how many photos a batch holds is not known
                before it is saved. */}
            <RotationControls
              order={background.unsplash.order}
              disabled={submitting}
              onOrder={(order) =>
                form.setFieldValue(
                  "appearance.background.unsplash.order",
                  order,
                )
              }
            >
              <form.Field
                name="appearance.background.unsplash.intervalSeconds"
                validators={{ onSubmit: ({ value }) => intervalError(value) }}
              >
                {(field) => (
                  <IntervalField
                    value={field.state.value}
                    disabled={submitting}
                    errors={field.state.meta.errors.flat(
                      Number.POSITIVE_INFINITY,
                    )}
                    onChange={(seconds) => field.handleChange(seconds)}
                  />
                )}
              </form.Field>
            </RotationControls>
          </>
        )}

        {background.source === "pixabay" && (
          <>
            {hasPixabayKey && (
              <SavedWallpaperKey
                source="pixabay"
                inUse={inUse === "pixabay"}
                disabled={submitting}
                replacing={replacingKey === "pixabay"}
                onReplace={() => onReplaceKey("pixabay")}
                onKeep={() => {
                  form.setFieldValue("pixabayApiKey", "");
                  onKeepKey("pixabay");
                }}
              />
            )}
            {(!hasPixabayKey || replacingKey === "pixabay") && (
              <form.AppField
                name="pixabayApiKey"
                validators={{
                  onSubmit: ({ value, fieldApi }) =>
                    keyError(value, {
                      source:
                        fieldApi.form.getFieldValue("appearance").background
                          .source,
                      service: "pixabay",
                      hasSavedKey: hasPixabayKey,
                    }),
                }}
              >
                {(field) => (
                  <field.FormField
                    label={
                      replacingKey === "pixabay" ? (
                        <Trans id="settings.sign-in.pixabay.key.new">
                          New API key
                        </Trans>
                      ) : (
                        <Trans id="settings.sign-in.pixabay.key">API key</Trans>
                      )
                    }
                    type="password"
                    autoComplete="off"
                    spellCheck={false}
                    isMonospace
                    compactValue
                    description={
                      <Trans id="settings.sign-in.pixabay.key.hint">
                        From your account on{" "}
                        <a
                          href="https://pixabay.com/api/docs/"
                          target="_blank"
                          rel="noreferrer"
                          className={inlineLinkClass}
                        >
                          pixabay.com/api/docs
                        </a>
                        .
                      </Trans>
                    }
                  />
                )}
              </form.AppField>
            )}
            <p className="text-muted-foreground text-caption">
              <Trans id="settings.sign-in.pixabay.width">
                Pixabay pictures are at most 1280 pixels wide, so they can look
                soft on large screens.
              </Trans>
            </p>
            <form.AppField
              name="appearance.background.pixabay.query"
              validators={{
                onBlur: ({ value }) => queryError(value),
                onSubmit: ({ value }) => queryError(value),
              }}
            >
              {(field) => (
                <field.FormField
                  label={
                    <Trans id="settings.sign-in.pixabay.query">Keyword</Trans>
                  }
                  placeholder="雪山"
                  autoComplete="off"
                  description={
                    <Trans id="settings.sign-in.pixabay.query.hint">
                      Written in the chosen search language; leave it empty for
                      any picture.
                    </Trans>
                  }
                />
              )}
            </form.AppField>
            <form.Field name="appearance.background.pixabay.lang">
              {(field) => (
                <LanguageSelect
                  value={field.state.value}
                  disabled={submitting}
                  onChange={(lang) => field.handleChange(lang)}
                />
              )}
            </form.Field>
            <RotationControls
              order={background.pixabay.order}
              disabled={submitting}
              onOrder={(order) =>
                form.setFieldValue("appearance.background.pixabay.order", order)
              }
            >
              <form.Field
                name="appearance.background.pixabay.intervalSeconds"
                validators={{ onSubmit: ({ value }) => intervalError(value) }}
              >
                {(field) => (
                  <IntervalField
                    value={field.state.value}
                    disabled={submitting}
                    errors={field.state.meta.errors.flat(
                      Number.POSITIVE_INFINITY,
                    )}
                    onChange={(seconds) => field.handleChange(seconds)}
                  />
                )}
              </form.Field>
            </RotationControls>
          </>
        )}

        {background.source === "images" && (
          <>
            <LoginImagesControl images={images} />
            {images.length === 0 && (
              <p className="text-muted-foreground text-sm">
                <Trans id="settings.sign-in.images.none_hint">
                  Until you add an image, visitors see the page's own
                  background.
                </Trans>
              </p>
            )}
            {images.length > 1 && (
              <RotationControls
                order={background.images.order}
                disabled={submitting}
                onOrder={(order) =>
                  form.setFieldValue(
                    "appearance.background.images.order",
                    order,
                  )
                }
              >
                <form.Field
                  name="appearance.background.images.intervalSeconds"
                  validators={{ onSubmit: ({ value }) => intervalError(value) }}
                >
                  {(field) => (
                    <IntervalField
                      value={field.state.value}
                      disabled={submitting}
                      errors={field.state.meta.errors.flat(
                        Number.POSITIVE_INFINITY,
                      )}
                      onChange={(seconds) => field.handleChange(seconds)}
                    />
                  )}
                </form.Field>
              </RotationControls>
            )}
          </>
        )}
      </>
    );
  },
});
