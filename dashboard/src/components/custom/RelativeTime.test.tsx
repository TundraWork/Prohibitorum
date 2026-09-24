import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, describe, expect, it, vi } from "vitest";
import { RelativeTime, relativeParts } from "@/components/custom/RelativeTime";
import { i18n } from "@/i18n";

const now = Date.parse("2026-09-10T12:00:00Z");
const ago = (seconds: number) => now - seconds * 1000;

describe("relativeParts", () => {
  it("names the distance in the largest unit that fits, without overflowing it", () => {
    expect(relativeParts(ago(59 * 60 + 59), now)).toEqual({
      value: -59,
      unit: "minute",
    });
    expect(relativeParts(ago(3 * 86400 + 3600), now)).toEqual({
      value: -3,
      unit: "day",
    });
    expect(relativeParts(ago(8 * 86400), now)).toEqual({
      value: -1,
      unit: "week",
    });
    expect(relativeParts(ago(400 * 86400), now)).toEqual({
      value: -1,
      unit: "year",
    });
    expect(relativeParts(now + 2 * 3600 * 1000, now)).toEqual({
      value: 2,
      unit: "hour",
    });
  });

  it("counts at least one second, so a fresh moment is never 'now'", () => {
    expect(relativeParts(now, now)).toEqual({ value: 1, unit: "second" });
    expect(relativeParts(ago(0.2), now)).toEqual({
      value: -1,
      unit: "second",
    });
  });
});

describe("RelativeTime", () => {
  afterEach(() => vi.useRealTimers());

  it("reads as a distance and puts the exact time in a tooltip", async () => {
    vi.useFakeTimers({ now, toFake: ["Date"] });
    i18n.activate("en");
    const user = userEvent.setup();
    render(
      <I18nProvider i18n={i18n}>
        <RelativeTime value={new Date(ago(86400)).toISOString()} />
      </I18nProvider>,
    );

    const time = screen.getByText("yesterday");
    expect(time.tagName).toBe("TIME");
    expect(time).toHaveAttribute("dateTime", "2026-09-09T12:00:00.000Z");

    // Keyboard focus opens the tooltip too; jsdom has no pointer to hover with.
    await user.tab();
    expect(time).toHaveFocus();
    expect(await screen.findByRole("tooltip")).toHaveTextContent("Sep 9, 2026");
  });
});
