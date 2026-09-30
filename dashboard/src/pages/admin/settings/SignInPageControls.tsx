import {
  type Color,
  ColorArea,
  ColorField,
  ColorPicker,
  ColorSlider,
  ColorSwatch,
  ColorSwatchPicker,
  Fieldset,
  Label,
  ListBox,
  parseColor,
  Radio,
  RadioGroup,
  Select,
  Slider,
  Switch,
} from "@heroui/react";
import { Trans, useLingui } from "@lingui/react/macro";
import {
  Blend,
  Camera,
  Images,
  type LucideIcon,
  PaintBucket,
  SquareDashed,
  Sunrise,
} from "lucide-react";
import { type ReactNode, useMemo } from "react";
import type {
  LoginBackgroundSource,
  LoginGradient,
  LoginSurface,
} from "@/api/raw-paths";
import {
  gradientBackground,
  loginGradients,
} from "@/components/custom/login-appearance/gradients";
import {
  backgroundSources,
  bingMarkets,
  suggestedColors,
} from "@/pages/admin/settings/sign-in-page-form";

const sourceIcons: Record<LoginBackgroundSource, LucideIcon> = {
  none: SquareDashed,
  color: PaintBucket,
  gradient: Blend,
  bing: Sunrise,
  unsplash: Camera,
  images: Images,
};

export function FieldGroup({
  legend,
  className = "flex flex-col gap-4",
  children,
}: {
  legend: ReactNode;
  className?: string;
  children: ReactNode;
}) {
  return (
    <Fieldset className={`min-w-0 ${className}`}>
      {/* The legend is not a flex item, so it keeps its own space below. */}
      <Fieldset.Legend className="mb-3 text-sm font-medium text-foreground">
        {legend}
      </Fieldset.Legend>
      {children}
    </Fieldset>
  );
}

/** The six sources as a 3×2 grid of tiles; the tile itself is the radio. */
export function SourceTiles({
  value,
  isDisabled,
  onChange,
}: {
  value: LoginBackgroundSource;
  isDisabled: boolean;
  onChange: (source: LoginBackgroundSource) => void;
}) {
  const { t } = useLingui();
  const names: Record<LoginBackgroundSource, string> = {
    none: t({ id: "settings.sign-in.source.none", message: "Default" }),
    color: t({ id: "settings.sign-in.source.color", message: "Color" }),
    gradient: t({
      id: "settings.sign-in.source.gradient",
      message: "Gradient",
    }),
    bing: "Bing",
    unsplash: "Unsplash",
    images: t({ id: "settings.sign-in.source.images", message: "Images" }),
  };
  return (
    <RadioGroup
      aria-label={t({
        id: "settings.sign-in.source",
        message: "Background source",
      })}
      variant="secondary"
      value={value}
      isDisabled={isDisabled}
      onChange={(next) => onChange(next as LoginBackgroundSource)}
      className="grid grid-cols-3 gap-2"
    >
      {backgroundSources.map((source) => {
        const Icon = sourceIcons[source];
        return (
          <Radio key={source} value={source} className="m-0">
            <Radio.Content className="group flex h-16 w-full cursor-pointer flex-col items-center justify-center gap-1.5 rounded-[0.375rem] bg-default/70 px-2 text-sm text-foreground/80 outline-none ring-accent transition-[background-color,box-shadow,color,scale] duration-150 ease-out data-[hovered=true]:bg-default data-[focus-visible=true]:ring-2 data-[focus-visible=true]:ring-focus data-[selected=true]:bg-accent-soft data-[selected=true]:font-medium data-[selected=true]:text-accent-soft-foreground data-[selected=true]:ring-1 data-[selected=true]:ring-inset data-[pressed=true]:scale-[0.98]">
              <Icon
                size={18}
                strokeWidth={1.75}
                aria-hidden="true"
                className="opacity-80 group-data-[selected=true]:opacity-100"
              />
              <span className="max-w-full truncate">{names[source]}</span>
            </Radio.Content>
          </Radio>
        );
      })}
    </RadioGroup>
  );
}

function readColor(hex: string): Color {
  try {
    return parseColor(hex);
  } catch {
    return parseColor("#1f6f8b");
  }
}

/** A picker, the hex value, and a row of suggestions; the value is lowercase `#rrggbb`. */
export function ColorControls({
  value,
  isDisabled,
  onChange,
}: {
  value: string;
  isDisabled: boolean;
  onChange: (hex: string) => void;
}) {
  const { t } = useLingui();
  const color = readColor(value);
  const set = (next: Color) => onChange(next.toString("hex").toLowerCase());
  const colorLabel = t({ id: "settings.sign-in.color", message: "Color" });
  return (
    <div className="flex flex-col gap-4">
      <div className="flex flex-col gap-2">
        <Label>{colorLabel}</Label>
        <div className="flex items-center gap-2">
          <ColorPicker value={color} onChange={set}>
            <ColorPicker.Trigger
              aria-label={colorLabel}
              isDisabled={isDisabled}
            >
              <ColorSwatch size="lg" className="rounded-[0.375rem]" />
            </ColorPicker.Trigger>
            <ColorPicker.Popover className="gap-3">
              <ColorArea
                aria-label={colorLabel}
                className="max-w-full"
                colorSpace="hsb"
                xChannel="saturation"
                yChannel="brightness"
              >
                <ColorArea.Thumb />
              </ColorArea>
              <ColorSlider
                channel="hue"
                colorSpace="hsb"
                className="gap-1 px-1"
              >
                <Label>
                  <Trans id="settings.sign-in.color.hue">Hue</Trans>
                </Label>
                <ColorSlider.Output className="text-muted" />
                <ColorSlider.Track>
                  <ColorSlider.Thumb />
                </ColorSlider.Track>
              </ColorSlider>
            </ColorPicker.Popover>
          </ColorPicker>
          <ColorField
            aria-label={t({
              id: "settings.sign-in.color.hex",
              message: "Hex value",
            })}
            value={color}
            isDisabled={isDisabled}
            onChange={(next) => {
              if (next) set(next);
            }}
            className="w-32"
          >
            <ColorField.Group variant="secondary">
              <ColorField.Input className="font-mono uppercase" />
            </ColorField.Group>
          </ColorField>
        </div>
      </div>
      <ColorSwatchPicker
        aria-label={t({
          id: "settings.sign-in.color.suggested",
          message: "Suggested colors",
        })}
        value={color}
        onChange={set}
        size="sm"
        variant="square"
        className="flex-wrap"
      >
        {suggestedColors.map((swatch) => (
          <ColorSwatchPicker.Item key={swatch} color={swatch}>
            <ColorSwatchPicker.Swatch />
            <ColorSwatchPicker.Indicator />
          </ColorSwatchPicker.Item>
        ))}
      </ColorSwatchPicker>
    </div>
  );
}

export function GradientControls({
  value,
  isDisabled,
  onChange,
}: {
  value: LoginGradient;
  isDisabled: boolean;
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
      variant="secondary"
      value={value}
      isDisabled={isDisabled}
      onChange={(next) => onChange(next as LoginGradient)}
      className="grid grid-cols-3 gap-x-3 gap-y-4"
    >
      {loginGradients.map((id) => (
        <Radio key={id} value={id} className="m-0">
          <Radio.Content className="group flex w-full cursor-pointer flex-col gap-2 outline-none">
            <span
              aria-hidden="true"
              data-gradient-swatch={id}
              className="aspect-[16/10] w-full rounded-[0.375rem] shadow-[inset_0_0_0_1px_oklch(0%_0_0/0.06)] ring-accent ring-offset-2 ring-offset-surface transition-shadow duration-150 group-data-[focus-visible=true]:ring-2 group-data-[focus-visible=true]:ring-focus group-data-[selected=true]:ring-2"
              style={{ background: gradientBackground[id] }}
            />
            <span className="text-sm text-foreground/80 group-data-[selected=true]:font-medium group-data-[selected=true]:text-foreground">
              {names[id]}
            </span>
          </Radio.Content>
        </Radio>
      ))}
    </RadioGroup>
  );
}

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

export function MarketSelect({
  value,
  isDisabled,
  onChange,
}: {
  value: string;
  isDisabled: boolean;
  onChange: (market: string) => void;
}) {
  const marketName = useMarketName();
  return (
    <Select
      variant="secondary"
      className="max-w-xs"
      value={value}
      isDisabled={isDisabled}
      onChange={(key) => {
        if (typeof key === "string") onChange(key);
      }}
    >
      <Label>
        <Trans id="settings.sign-in.bing.region">Region</Trans>
      </Label>
      <Select.Trigger>
        <Select.Value />
        <Select.Indicator />
      </Select.Trigger>
      <Select.Popover>
        <ListBox>
          {bingMarkets.map((market) => (
            <ListBox.Item
              id={market}
              key={market}
              textValue={marketName(market)}
            >
              {marketName(market)}
              <ListBox.ItemIndicator />
            </ListBox.Item>
          ))}
        </ListBox>
      </Select.Popover>
    </Select>
  );
}

/**
 * Whether the card or the toolbar is translucent. Opacity and frosted glass
 * appear only while it is; turning it off keeps both values for next time.
 */
export function SurfaceControls({
  legend,
  value,
  isDisabled,
  onChange,
}: {
  legend: ReactNode;
  value: LoginSurface;
  isDisabled: boolean;
  onChange: (surface: LoginSurface) => void;
}) {
  return (
    <FieldGroup legend={legend} className="flex flex-col gap-3">
      <Switch
        isSelected={value.translucent}
        isDisabled={isDisabled}
        onChange={(translucent) => onChange({ ...value, translucent })}
      >
        <Switch.Content>
          <Switch.Control>
            <Switch.Thumb />
          </Switch.Control>
          <Trans id="settings.sign-in.surface.translucent">Translucent</Trans>
        </Switch.Content>
      </Switch>
      {value.translucent && (
        <>
          <Slider
            value={value.opacity}
            minValue={0}
            maxValue={100}
            step={5}
            isDisabled={isDisabled}
            onChange={(next) =>
              onChange({
                ...value,
                opacity: Array.isArray(next) ? (next[0] ?? 0) : next,
              })
            }
            formatOptions={{ style: "unit", unit: "percent" }}
          >
            <Label>
              <Trans id="settings.sign-in.surface.opacity">Opacity</Trans>
            </Label>
            <Slider.Output className="tabular-nums" />
            <Slider.Track>
              <Slider.Fill />
              <Slider.Thumb />
            </Slider.Track>
          </Slider>
          <Switch
            isSelected={value.blur}
            isDisabled={isDisabled}
            onChange={(blur) => onChange({ ...value, blur })}
          >
            <Switch.Content>
              <Switch.Control>
                <Switch.Thumb />
              </Switch.Control>
              <Trans id="settings.sign-in.surface.blur">Frosted glass</Trans>
            </Switch.Content>
          </Switch>
        </>
      )}
    </FieldGroup>
  );
}
