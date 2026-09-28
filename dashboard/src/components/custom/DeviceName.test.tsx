import { I18nProvider } from "@lingui/react";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it } from "vitest";
import { DeviceName, describeDevice } from "@/components/custom/DeviceName";
import { i18n } from "@/i18n";

const firefoxLinux =
  "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0";
const safariIphone =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1";
const chromeWindows =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36";
const safariIpad =
  "Mozilla/5.0 (iPad; CPU OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1";

beforeEach(() => i18n.activate("en"));

function mount(userAgent: string | undefined) {
  render(
    <I18nProvider i18n={i18n}>
      <DeviceName userAgent={userAgent} />
    </I18nProvider>,
  );
}

describe("describeDevice", () => {
  it("names the browser and the system, and the kind of device", () => {
    expect(describeDevice(firefoxLinux)).toEqual({
      name: "Firefox · Linux",
      kind: "desktop",
    });
    expect(describeDevice(safariIphone)).toEqual({
      name: "Safari · iOS",
      kind: "mobile",
    });
    expect(describeDevice(chromeWindows)).toEqual({
      name: "Chrome · Windows",
      kind: "desktop",
    });
    expect(describeDevice(safariIpad)).toEqual({
      name: "Safari · iOS",
      kind: "tablet",
    });
  });

  it("gives no name when not even the browser is recognised", () => {
    expect(describeDevice("curl/8.5.0")).toEqual({
      name: null,
      kind: "unknown",
    });
    expect(describeDevice("")).toEqual({ name: null, kind: "unknown" });
    expect(describeDevice(undefined)).toEqual({ name: null, kind: "unknown" });
  });
});

describe("DeviceName", () => {
  it("shows the parsed name and keeps the raw string in the tooltip", async () => {
    const user = userEvent.setup();
    mount(firefoxLinux);

    const name = screen.getByText("Firefox · Linux");
    await user.tab();
    expect(name).toHaveFocus();
    expect(await screen.findByRole("tooltip")).toHaveTextContent(firefoxLinux);
  });

  it("shows an unreadable string as it is", () => {
    mount("curl/8.5.0");
    expect(screen.getByText("curl/8.5.0")).toBeInTheDocument();
  });

  it("cuts an unreadable string past 80 characters, keeping all of it in the tooltip", async () => {
    const long = `curl/${"x".repeat(120)}`;
    const user = userEvent.setup();
    mount(long);

    const shown = screen.getByText(`${long.slice(0, 79)}…`);
    expect(shown.textContent).toHaveLength(80);
    await user.tab();
    expect(await screen.findByRole("tooltip")).toHaveTextContent(long);
  });

  it("says the device is unknown when there is no User-Agent at all", () => {
    mount("");
    expect(screen.getByText("Unknown device")).toBeInTheDocument();
  });
});
