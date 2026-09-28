import { Tooltip } from "@heroui/react";
import { Trans } from "@lingui/react/macro";
import Bowser from "bowser";
import { Monitor, MonitorSmartphone, Smartphone, Tablet } from "lucide-react";

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
export function DeviceName({ userAgent }: { userAgent: string | undefined }) {
  if (!userAgent) return <Trans id="device.unknown">Unknown device</Trans>;
  const { name } = describeDevice(userAgent);
  const shown =
    name ??
    (userAgent.length > rawLimit
      ? `${userAgent.slice(0, rawLimit - 1)}…`
      : userAgent);
  return (
    <Tooltip delay={0}>
      <Tooltip.Trigger<"span"> render={(props) => <span {...props} />}>
        {shown}
      </Tooltip.Trigger>
      <Tooltip.Content className="break-all">{userAgent}</Tooltip.Content>
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
  return <Icon size={18} aria-hidden="true" />;
}
