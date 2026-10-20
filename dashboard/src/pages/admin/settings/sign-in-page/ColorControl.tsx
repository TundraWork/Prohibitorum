import { Trans, useLingui } from "@lingui/react/macro";
import { useRef } from "react";
import { FormMessages } from "@/components/custom/FormMessages";
import { Field, FieldError, FieldLabel } from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  RadioGroupPrimitive,
  RadioPrimitive,
} from "@/components/ui/radio-group";
import { Slider } from "@/components/ui/slider";
import { hexToHsb, withHue } from "@/pages/admin/settings/sign-in-page/color";
import { suggestedColors } from "@/pages/admin/settings/sign-in-page/sign-in-page-form";

const hexColor = /^#[0-9a-f]{6}$/;

/** Each suggestion's name for screen readers, in the console's language. */
function useSwatchNames(): Record<string, string> {
  const { t } = useLingui();
  return {
    "#1f6f8b": t({ id: "settings.sign-in.color.teal", message: "Teal" }),
    "#3b3f58": t({ id: "settings.sign-in.color.slate", message: "Slate" }),
    "#4c5d8a": t({ id: "settings.sign-in.color.indigo", message: "Indigo" }),
    "#5b7a5a": t({ id: "settings.sign-in.color.sage", message: "Sage" }),
    "#8a5a44": t({ id: "settings.sign-in.color.clay", message: "Clay" }),
    "#101418": t({
      id: "settings.sign-in.color.near_black",
      message: "Near black",
    }),
    "#b9c4cc": t({
      id: "settings.sign-in.color.pale_blue",
      message: "Pale blue-grey",
    }),
    "#efe9df": t({ id: "settings.sign-in.color.cream", message: "Cream" }),
  };
}

/** The hue slider's track, so the colours it moves through are the ones it shows. */
const hueTrack =
  "[&_[data-slot=slider-track]]:h-2 [&_[data-slot=slider-track]]:rounded-full [&_[data-slot=slider-track]]:bg-[linear-gradient(to_right,#f00,#ff0,#0f0,#0ff,#00f,#f0f,#f00)] [&_[data-slot=slider-track]]:before:hidden [&_[data-slot=slider-indicator]]:hidden";

/**
 * A background colour: the hex value, the browser's own picker beside it, a
 * hue slider and a row of suggestions. The value is whatever was typed, in
 * lowercase, so the form can say what is wrong with it rather than the control
 * quietly keeping the last good colour; `errors` are the form field's.
 */
export function ColorControl({
  value,
  errors,
  disabled,
  onChange,
}: {
  value: string;
  errors: readonly unknown[];
  disabled: boolean;
  onChange: (hex: string) => void;
}) {
  const { t } = useLingui();
  const valid = hexColor.test(value);
  // A colour half typed leaves the picker on the last one that was whole.
  const lastValid = useRef("#1f6f8b");
  if (valid) lastValid.current = value;
  const swatchNames = useSwatchNames();
  const hue = hexToHsb(valid ? value : lastValid.current).h;
  return (
    <div className="flex flex-col gap-4">
      <Field disabled={disabled} invalid={errors.length > 0} className="gap-2">
        <FieldLabel>
          <Trans id="settings.sign-in.color">Color</Trans>
        </FieldLabel>
        <div className="flex w-full items-center gap-2">
          <input
            type="color"
            aria-label={t({
              id: "settings.sign-in.color.picker",
              message: "Pick a color",
            })}
            value={valid ? value : lastValid.current}
            disabled={disabled}
            onChange={(event) => onChange(event.target.value.toLowerCase())}
            className="size-9 shrink-0 cursor-pointer rounded-lg border border-input bg-transparent p-0.5 disabled:cursor-not-allowed disabled:opacity-64"
          />
          <Input
            value={value}
            onValueChange={(next) =>
              // Typed without its `#`, a colour still reads as one.
              onChange(
                next === "" || next.startsWith("#")
                  ? next.toLowerCase()
                  : `#${next.toLowerCase()}`,
              )
            }
            autoComplete="off"
            spellCheck={false}
            aria-invalid={errors.length > 0 || undefined}
            className="font-mono text-base sm:text-[13px]"
          />
        </div>
        {errors.length > 0 && (
          <FieldError match>
            <FormMessages errors={errors} />
          </FieldError>
        )}
      </Field>

      <div className="flex flex-col gap-2">
        <div className="flex items-baseline justify-between gap-4">
          <span className="font-medium text-sm">
            <Trans id="settings.sign-in.color.hue">Hue</Trans>
          </span>
          <span className="font-mono text-muted-foreground text-xs">
            {hue}°
          </span>
        </div>
        <Slider
          value={hue}
          min={0}
          max={360}
          step={1}
          disabled={disabled}
          className={hueTrack}
          getAriaLabel={() =>
            t({ id: "settings.sign-in.color.hue", message: "Hue" })
          }
          onValueChange={(next) =>
            onChange(
              withHue(value, Array.isArray(next) ? (next[0] ?? 0) : next),
            )
          }
        />
      </div>

      <RadioGroupPrimitive
        aria-label={t({
          id: "settings.sign-in.color.suggested",
          message: "Suggested colors",
        })}
        value={value}
        disabled={disabled}
        onValueChange={(next) => onChange(String(next))}
        className="flex flex-wrap gap-2"
      >
        {suggestedColors.map((swatch) => (
          <RadioPrimitive.Root
            key={swatch}
            value={swatch}
            aria-label={swatchNames[swatch]}
            style={{ backgroundColor: swatch }}
            className="size-7 cursor-pointer rounded-md ring-1 ring-black/10 ring-inset outline-none transition-shadow focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 focus-visible:ring-offset-background data-checked:ring-2 data-checked:ring-primary data-checked:ring-offset-2 data-checked:ring-offset-background data-disabled:cursor-not-allowed data-disabled:opacity-64 dark:ring-white/20"
          />
        ))}
      </RadioGroupPrimitive>
    </div>
  );
}
