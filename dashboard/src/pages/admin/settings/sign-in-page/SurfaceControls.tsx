import { Trans, useLingui } from "@lingui/react/macro";
import { useId } from "react";
import type {
  LoginCardPosition,
  LoginSurface,
  LoginTheme,
} from "@/api/raw-paths";
import { Field, FieldLabel } from "@/components/ui/field";
import { Slider } from "@/components/ui/slider";
import { Switch } from "@/components/ui/switch";
import { ChoiceStrip } from "@/pages/admin/settings/sign-in-page/ChoiceStrip";

/** Where the card sits on a wide window and which theme the public pages wear. */
export function PlacementControls({
  position,
  theme,
  disabled,
  onPosition,
  onTheme,
}: {
  position: LoginCardPosition;
  theme: LoginTheme;
  disabled: boolean;
  onPosition: (position: LoginCardPosition) => void;
  onTheme: (theme: LoginTheme) => void;
}) {
  const { t } = useLingui();
  return (
    <>
      <ChoiceStrip
        label={t({ id: "settings.sign-in.position", message: "Position" })}
        value={position}
        disabled={disabled}
        onChange={onPosition}
        options={[
          {
            value: "left",
            label: t({
              id: "settings.sign-in.position.left",
              message: "Left",
            }),
          },
          {
            value: "center",
            label: t({
              id: "settings.sign-in.position.center",
              message: "Center",
            }),
          },
          {
            value: "right",
            label: t({
              id: "settings.sign-in.position.right",
              message: "Right",
            }),
          },
        ]}
      />
      <ChoiceStrip
        label={t({ id: "settings.sign-in.theme", message: "Theme" })}
        value={theme}
        disabled={disabled}
        onChange={onTheme}
        options={[
          {
            value: "switchable",
            label: t({
              id: "settings.sign-in.theme.switchable",
              message: "Visitor's choice",
            }),
          },
          {
            value: "light",
            label: t({
              id: "settings.sign-in.theme.light",
              message: "Always light",
            }),
          },
          {
            value: "dark",
            label: t({
              id: "settings.sign-in.theme.dark",
              message: "Always dark",
            }),
          },
        ]}
      />
    </>
  );
}

/**
 * Whether the card or the toolbar is translucent. Opacity and frosted glass
 * appear only while it is; turning it off keeps both values for next time.
 */
export function SurfaceFields({
  surface,
  disabled,
  onChange,
}: {
  surface: LoginSurface;
  disabled: boolean;
  onChange: (surface: LoginSurface) => void;
}) {
  const { t } = useLingui();
  const opacityId = useId();
  return (
    <>
      <Field disabled={disabled}>
        <div className="flex w-full items-center justify-between gap-4">
          <FieldLabel>
            <Trans id="settings.sign-in.surface.translucent">Translucent</Trans>
          </FieldLabel>
          <Switch
            checked={surface.translucent}
            onCheckedChange={(translucent) =>
              onChange({ ...surface, translucent })
            }
          />
        </div>
      </Field>
      {surface.translucent && (
        <>
          <div className="flex flex-col gap-2">
            <div className="flex items-baseline justify-between gap-4">
              <span id={opacityId} className="font-medium text-sm">
                <Trans id="settings.sign-in.surface.opacity">Opacity</Trans>
              </span>
              <span className="font-mono text-muted-foreground text-xs">
                {surface.opacity}%
              </span>
            </div>
            <Slider
              value={surface.opacity}
              min={0}
              max={100}
              step={5}
              disabled={disabled}
              getAriaLabel={() =>
                t({
                  id: "settings.sign-in.surface.opacity",
                  message: "Opacity",
                })
              }
              onValueChange={(next) =>
                onChange({
                  ...surface,
                  opacity: Array.isArray(next) ? (next[0] ?? 0) : next,
                })
              }
            />
          </div>
          <Field disabled={disabled}>
            <div className="flex w-full items-center justify-between gap-4">
              <FieldLabel>
                <Trans id="settings.sign-in.surface.blur">Frosted glass</Trans>
              </FieldLabel>
              <Switch
                checked={surface.blur}
                onCheckedChange={(blur) => onChange({ ...surface, blur })}
              />
            </div>
          </Field>
        </>
      )}
    </>
  );
}
