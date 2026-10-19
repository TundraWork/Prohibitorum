import { Trans, useLingui } from "@lingui/react/macro";
import Bowser from "bowser";
import { Monitor, MonitorSmartphone, Smartphone, Tablet } from "lucide-react";
import { Tooltip, TooltipPopup, TooltipTrigger } from "@/components/ui/tooltip";
import { cn } from "@/lib/utils";

export type DeviceKind = "desktop" | "mobile" | "tablet" | "unknown";

/** How long an unreadable User-Agent may run on a row before it is cut. */
const rawLimit = 80;

/**
 * A User-Agent read into what a person calls the device: "Firefox · Linux".
 *
 * `name` is `null` when not even the browser can be recognised, so the caller
 * shows the raw string rather than a guess. Browser and system names are
 * proper nouns and stay as bowser spells them, in every locale.
 */
export function describeDevice(userAgent: string | undefined): {
  name: string | null;
  kind: DeviceKind;
} {
  // bowser throws on an empty string rather than returning nothing.
  if (!userAgent) return { name: null, kind: "unknown" };
  let parsed: Bowser.Parser.ParsedResult;
  try {
    parsed = Bowser.parse(userAgent);
  } catch {
    return { name: null, kind: "unknown" };
  }
  const browser = parsed.browser.name;
  const system = parsed.os.name;
  const type = parsed.platform.type;
  const kind: DeviceKind =
    type === "desktop" || type === "mobile" || type === "tablet"
      ? type
      : "unknown";
  if (!browser) return { name: null, kind };
  return { name: system ? `${browser} · ${system}` : browser, kind };
}

/**
 * The device a session or a pairing request came from, by name, with the
 * whole User-Agent in a tooltip for the reader who needs to check it.
 */
export function DeviceName({
  userAgent,
  className,
}: {
  userAgent: string | undefined;
  /** Layout for the name, such as `block truncate` to keep it to one line. */
  className?: string;
}) {
  if (!userAgent) return <Trans id="device.unknown">Unknown device</Trans>;
  const { name } = describeDevice(userAgent);
  const shown =
    name ??
    (userAgent.length > rawLimit
      ? `${userAgent.slice(0, rawLimit - 1)}…`
      : userAgent);
  return (
    <Tooltip>
      <TooltipTrigger
        delay={0}
        render={
          // Focusable so the full User-Agent is not hover-only.
          <span
            // biome-ignore lint/a11y/noNoninteractiveTabindex: the tooltip is the only way to read the full User-Agent
            tabIndex={0}
            className={cn(
              "rounded-xs outline-none focus-visible:ring-2 focus-visible:ring-ring",
              className,
            )}
          />
        }
      >
        {shown}
      </TooltipTrigger>
      <TooltipPopup className="max-w-xs break-all">{userAgent}</TooltipPopup>
    </Tooltip>
  );
}

/** The icon for the kind of device a User-Agent names. */
export function DeviceIcon({ userAgent }: { userAgent: string | undefined }) {
  const { kind } = describeDevice(userAgent);
  const Icon =
    kind === "desktop"
      ? Monitor
      : kind === "mobile"
        ? Smartphone
        : kind === "tablet"
          ? Tablet
          : MonitorSmartphone;
  return <Icon aria-hidden="true" />;
}

/**
 * The whole User-Agent as a monospace detail, for one the row's title could
 * not name; `null` when the title already names the device. A function rather
 * than a component, so a list row's details can drop it instead of drawing an
 * empty entry.
 */
export function rawUserAgentDetail(userAgent: string | undefined) {
  if (!userAgent || describeDevice(userAgent).name !== null) return null;
  return (
    <span key="ua" className="font-mono">
      {userAgent}
    </span>
  );
}

/**
 * The device in words for a control's name: the browser and system when they
 * can be read, otherwise the User-Agent cut as `DeviceName` cuts it.
 */
export function useDeviceLabel() {
  const { t } = useLingui();
  return (userAgent: string | undefined): string => {
    if (!userAgent)
      return t({ id: "device.unknown", message: "Unknown device" });
    const { name } = describeDevice(userAgent);
    return (
      name ??
      (userAgent.length > rawLimit
        ? `${userAgent.slice(0, rawLimit - 1)}…`
        : userAgent)
    );
  };
}
