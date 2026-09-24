import { Tooltip } from "@heroui/react";
import { useLingui } from "@lingui/react/macro";
import { useEffect, useState } from "react";

const MINUTE = 60;
const HOUR = 60 * MINUTE;
const DAY = 24 * HOUR;

/** Largest unit first; a month and a year are the average lengths. */
const UNITS: readonly [Intl.RelativeTimeFormatUnit, number][] = [
  ["year", 365.25 * DAY],
  ["month", 30.44 * DAY],
  ["week", 7 * DAY],
  ["day", DAY],
  ["hour", HOUR],
  ["minute", MINUTE],
];

/**
 * The distance from `now` to `then` in the largest unit that fits, so a
 * moment reads "3 days ago" rather than "72 hours ago". The count is cut, not
 * rounded, so it never reaches the next unit ("60 minutes ago"); under a
 * minute it is at least one second, so a detail never reads "used now".
 */
export function relativeParts(
  then: number,
  now: number,
): { value: number; unit: Intl.RelativeTimeFormatUnit } {
  const seconds = (then - now) / 1000;
  const size = Math.abs(seconds);
  for (const [unit, length] of UNITS) {
    if (size >= length) return { value: Math.trunc(seconds / length), unit };
  }
  const whole = Math.max(1, Math.trunc(size));
  return { value: seconds < 0 ? -whole : whole, unit: "second" };
}

/** How long the text can stand before its unit ticks over. */
function refreshAfter(then: number, now: number): number {
  const size = Math.abs(then - now) / 1000;
  if (size < MINUTE) return 1000;
  if (size < HOUR) return MINUTE * 1000;
  if (size < DAY) return HOUR * 1000;
  return DAY * 1000;
}

/** How long a pointer has to rest on the time before the tooltip opens. */
const hoverDelay = 600;

/**
 * A moment said as a distance from now — "3 days ago", "in 2 hours" — with
 * the exact date and time in a tooltip. For the times a reader asks "how long
 * ago?" of: last used, last signed in, started. A date the reader needs to
 * plan around, such as when something expires, stays a date.
 *
 * The text keeps itself current: it re-renders when its unit would tick over,
 * so a row left open does not keep saying "now".
 */
export function RelativeTime({ value }: { value: string }) {
  const { i18n } = useLingui();
  const then = new Date(value).getTime();
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    const timer = window.setTimeout(
      () => setNow(Date.now()),
      refreshAfter(then, now),
    );
    return () => window.clearTimeout(timer);
  }, [then, now]);

  if (Number.isNaN(then)) return null;
  const { value: amount, unit } = relativeParts(then, now);
  // "auto" says "yesterday" and "last week"; seconds stay numeric, since
  // "auto" would make one second "now".
  const relative = new Intl.RelativeTimeFormat(i18n.locale, {
    numeric: unit === "second" ? "always" : "auto",
  }).format(amount, unit);
  const exact = new Intl.DateTimeFormat(i18n.locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(then);

  return (
    <Tooltip delay={hoverDelay}>
      <Tooltip.Trigger<"time">
        render={(props) => <time {...props} dateTime={value} />}
      >
        {relative}
      </Tooltip.Trigger>
      <Tooltip.Content placement="bottom">{exact}</Tooltip.Content>
    </Tooltip>
  );
}
