import { I18nProvider } from "@lingui/react";
import { render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { i18n } from "@/i18n";

const { toCanvas } = vi.hoisted(() => ({
  toCanvas:
    vi.fn<
      (
        element: HTMLCanvasElement,
        uri: string,
        options?: unknown,
      ) => Promise<void>
    >(),
}));
vi.mock("qrcode", () => ({ default: { toCanvas } }));

import { TotpSetup } from "@/components/custom/TotpSetup";

const uriA =
  "otpauth://totp/Prohibitorum:alice?secret=AAAA&issuer=Prohibitorum";
const uriB =
  "otpauth://totp/Prohibitorum:alice?secret=BBBB&issuer=Prohibitorum";

function setup(uri: string) {
  return (
    <I18nProvider i18n={i18n}>
      <TotpSetup secret="AAAA" uri={uri} />
    </I18nProvider>
  );
}

const qrFailedWarning = /could not be displayed/i;

beforeEach(() => {
  i18n.activate("en");
  toCanvas.mockReset();
  toCanvas.mockResolvedValue(undefined);
});

describe("TotpSetup", () => {
  it("draws the QR when the uri arrives after a first render without one", async () => {
    // The panel mounts before the config query lands, so `uri` is empty at
    // first. An empty uri is not a failure and must not take the canvas away.
    const view = render(setup(""));
    expect(toCanvas).not.toHaveBeenCalled();

    view.rerender(setup(uriA));

    await waitFor(() => expect(toCanvas).toHaveBeenCalled());
    const [canvas, uri] = toCanvas.mock.calls.at(-1) ?? [];
    expect(uri).toBe(uriA);
    expect(screen.getByRole("img", { name: /setup QR code/i })).toBe(canvas);
  });

  it("keeps the canvas mounted so a later uri replaces a failed QR", async () => {
    toCanvas.mockRejectedValueOnce(new Error("boom"));
    const view = render(setup(uriA));
    await screen.findByText(qrFailedWarning);

    view.rerender(setup(uriB));

    await waitFor(() =>
      expect(screen.queryByText(qrFailedWarning)).not.toBeInTheDocument(),
    );
    const [canvas, uri] = toCanvas.mock.calls.at(-1) ?? [];
    expect(uri).toBe(uriB);
    expect(screen.getByRole("img", { name: /setup QR code/i })).toBe(canvas);
  });
});
