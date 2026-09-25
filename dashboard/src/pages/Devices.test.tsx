import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { DevicePairing } from "@/api/raw-paths";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { Devices } from "@/pages/Devices";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
let queryClient: QueryClient;

const pairing: DevicePairing = {
  pairingId: "pairing-1",
  displayCode: "ABCD-2345",
  initiatorUa: "Mozilla/5.0 (X11; Linux x86_64) TestBrowser/1.0",
  initiatorIp: "198.51.100.7",
  createdAt: new Date(Date.now() - 60_000).toISOString(),
  expiresAt: new Date(Date.now() + 9 * 60_000).toISOString(),
  alreadyBound: false,
};

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(() => undefined, notifySuccess);
  configureSudo({
    queryClient,
    set: () => {},
    setFresh: () => {},
    getFresh: () => true,
  });
});

afterEach(() => {
  resetSudo();
  queryClient.clear();
  vi.unstubAllGlobals();
});

function serve(lookup: () => Response) {
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/devices/pair/lookup")) return lookup();
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    return new Response(null, { status: 204 });
  });
}

function mount() {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Devices />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function requests(suffix: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => new URL(request.url).pathname.endsWith(suffix));
}

async function pair(code: string) {
  const user = userEvent.setup();
  mount();
  await user.type(screen.getByLabelText("Code from the other device"), code);
  await user.click(screen.getByRole("button", { name: "Pair" }));
  return user;
}

describe("device pairing", () => {
  it("asks for a code before looking anything up", async () => {
    serve(() => Response.json(pairing));
    const user = userEvent.setup();
    mount();

    await user.click(screen.getByRole("button", { name: "Pair" }));

    expect(
      await screen.findByText(
        "Enter the pairing code shown on the other device.",
      ),
    ).toBeInTheDocument();
    expect(fetchBoundary).not.toHaveBeenCalled();
  });

  it("shows who is asking in a dialog, and approving closes it with a toast", async () => {
    serve(() => Response.json(pairing));
    const user = await pair("ABCD2345");

    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByRole("heading", { name: "Approve this device?" }),
    ).toBeInTheDocument();
    expect(within(dialog).getByText(pairing.initiatorUa)).toBeInTheDocument();
    expect(within(dialog).getByText(pairing.initiatorIp)).toBeInTheDocument();
    const lookup = requests("/pair/lookup")[0];
    expect(new URL(lookup?.url ?? "").searchParams.get("code")).toBe(
      "ABCD2345",
    );

    await user.click(within(dialog).getByRole("button", { name: "Approve" }));

    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    const approve = requests("/pair/approve");
    expect(approve).toHaveLength(1);
    expect(await approve[0]?.json()).toEqual({ code: "ABCD2345" });
    expect(notifySuccess).toHaveBeenCalledWith(
      expect.objectContaining({ id: "success.device.approved" }),
    );
    // The form is ready for the next device.
    expect(screen.getByLabelText("Code from the other device")).toHaveValue("");
  });

  it("declines from the same dialog", async () => {
    serve(() => Response.json(pairing));
    const user = await pair("ABCD2345");

    const dialog = await screen.findByRole("dialog");
    await user.click(within(dialog).getByRole("button", { name: "Decline" }));

    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(requests("/pair/cancel")).toHaveLength(1);
    expect(requests("/pair/approve")).toHaveLength(0);
    expect(notifySuccess).toHaveBeenCalledWith(
      expect.objectContaining({ id: "success.device.declined" }),
    );
  });

  it("puts an unknown or expired code on the field instead of opening the dialog", async () => {
    serve(() =>
      Response.json(
        { code: "pairing_not_found", requestId: "devices-test" },
        { status: 404 },
      ),
    );
    await pair("ABCD2345");

    expect(
      await screen.findByText(
        "That pairing code is not valid. It may have been used already, or it may have expired.",
      ),
    ).toBeInTheDocument();
    expect(screen.getByLabelText("Code from the other device")).toHaveAttribute(
      "aria-invalid",
      "true",
    );
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("does not offer to approve a pairing this account already approved", async () => {
    serve(() => Response.json({ ...pairing, alreadyBound: true }));
    await pair("ABCD2345");

    const dialog = await screen.findByRole("dialog");
    expect(
      within(dialog).getByText(
        "You have already approved this pairing. The other device should be finishing its setup.",
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Approve" }),
    ).toBeDisabled();
    expect(
      within(dialog).getByRole("button", { name: "Decline" }),
    ).toBeEnabled();
  });
});
