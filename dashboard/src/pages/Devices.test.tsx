import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { components } from "@/api/generated/schema";
import type { DevicePairing } from "@/api/raw-paths";
import { configureSudo, resetSudo } from "@/api/sudo";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { Devices } from "@/pages/Devices";
import { devicesSearch } from "@/pages/devices/pairing-code";
import { firstOtpSlot, otpValue } from "@/test/otp";

type Session = components["schemas"]["SessionListItem"];

const firefoxLinux =
  "Mozilla/5.0 (X11; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0";
const safariIphone =
  "Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1";
const chromeWindows =
  "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifySuccess = vi.fn<(message: MessageDescriptor) => void>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;
let sessions: Session[];

function session(index: number, userAgent: string): Session {
  return {
    id: `session-${index}`,
    isCurrent: index === 1,
    issuedAt: new Date(Date.now() - index * 86_400_000).toISOString(),
    expiresAt: new Date(Date.now() + 30 * 86_400_000).toISOString(),
    lastSeenIp: `192.0.2.${index}`,
    userAgent,
  };
}

function pairing(overrides: Partial<DevicePairing> = {}): DevicePairing {
  return {
    pairingId: "pairing-1",
    displayCode: "ABCD-2345",
    initiatorUa: chromeWindows,
    initiatorIp: "198.51.100.7",
    createdAt: new Date(Date.now() - 60_000).toISOString(),
    expiresAt: new Date(Date.now() + 4 * 60_000).toISOString(),
    alreadyBound: false,
    ...overrides,
  };
}

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifySuccess.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(notifyError, notifySuccess);
  sessions = [session(1, firefoxLinux), session(2, safariIphone)];
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
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

function serve(
  lookup: () => Response,
  writes: (path: string) => Response | undefined = () => undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const path = new URL(request.url).pathname;
    if (path.endsWith("/me/devices/pair/lookup")) return lookup();
    if (path.endsWith("/me/sessions")) return Response.json(sessions);
    if (path.endsWith("/me/sudo/methods")) {
      return Response.json({ methods: ["password_totp"], fresh: true });
    }
    return writes(path) ?? new Response(null, { status: 204 });
  });
}

function mount(code?: string) {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <Devices code={code} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function requests(suffix: string) {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => new URL(request.url).pathname.endsWith(suffix));
}

type User = ReturnType<typeof userEvent.setup>;

async function openPairing(user: User) {
  await user.click(
    await screen.findByRole("button", { name: "Sign in a new device" }),
  );
  return screen.findByRole("dialog", { name: "Sign in a new device" });
}

/** Opens the dialog, enters `code` and continues; returns the dialog. */
async function enterCode(user: User, code: string) {
  const dialog = await openPairing(user);
  if (code !== "") {
    await user.type(
      firstOtpSlot("Pairing code shown on the new device", within(dialog)),
      code,
    );
  }
  await user.click(within(dialog).getByRole("button", { name: "Continue" }));
  return dialog;
}

describe("signed-in devices", () => {
  it("names each device and explains why this one has no end action", async () => {
    serve(() => Response.json(pairing()));
    mount();

    const list = await screen.findByRole("list", { name: "Signed-in devices" });
    const [current, other] = within(list).getAllByRole("listitem");
    expect(within(current as HTMLElement).getByText("Firefox · Linux"));
    expect(
      within(current as HTMLElement).getByText("This device"),
    ).toBeInTheDocument();
    expect(within(other as HTMLElement).getByText("Safari · iOS"));
    expect(
      within(other as HTMLElement).queryByText("This device"),
    ).not.toBeInTheDocument();

    // The current row says how to end it instead of offering a dead button.
    expect(
      within(current as HTMLElement).getByText("Use sign out to end this one"),
    ).toBeInTheDocument();
    expect(
      within(current as HTMLElement).queryByRole("button", {
        name: "End session",
      }),
    ).not.toBeInTheDocument();
    expect(
      within(other as HTMLElement).getByRole("button", { name: "End session" }),
    ).toBeEnabled();
    expect(
      within(other as HTMLElement).queryByText("Use sign out to end this one"),
    ).not.toBeInTheDocument();
  });

  it("shows the address and when each session signed in", async () => {
    serve(() => Response.json(pairing()));
    mount();

    const list = await screen.findByRole("list", { name: "Signed-in devices" });
    const [, other] = within(list).getAllByRole("listitem");
    expect(
      within(other as HTMLElement).getByText("192.0.2.2"),
    ).toBeInTheDocument();
    expect(
      within(other as HTMLElement).getByText(/Signed in/),
    ).toBeInTheDocument();
  });

  it("keeps the whole User-Agent in the details when it cannot be read", async () => {
    const curl = `curl/${"x".repeat(120)}`;
    sessions = [session(1, firefoxLinux), session(2, curl)];
    serve(() => Response.json(pairing()));
    mount();

    const list = await screen.findByRole("list", { name: "Signed-in devices" });
    const [, other] = within(list).getAllByRole("listitem");
    const details = (other as HTMLElement).querySelector(
      '[data-slot="item-details"]',
    );
    expect(details).toHaveTextContent(curl);
  });

  it("ends another session after confirming", async () => {
    serve(
      () => Response.json(pairing()),
      () => new Response(null, { status: 204 }),
    );
    const user = userEvent.setup();
    mount();

    const list = await screen.findByRole("list", { name: "Signed-in devices" });
    const [, other] = within(list).getAllByRole("listitem");
    await user.click(
      within(other as HTMLElement).getByRole("button", { name: "End session" }),
    );
    const confirm = await screen.findByRole("alertdialog", {
      name: "End this session?",
    });
    await user.click(
      within(confirm).getByRole("button", { name: "End session" }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(requests("/me/sessions/revoke")).toHaveLength(1);
  });

  it("reports a failed end once, in the page, and not as a toast", async () => {
    serve(
      () => Response.json(pairing()),
      (path) =>
        path.includes("/revoke")
          ? Response.json(
              { code: "session_not_found", requestId: "devices-test" },
              { status: 404 },
            )
          : undefined,
    );
    const user = userEvent.setup();
    mount();

    const list = await screen.findByRole("list", { name: "Signed-in devices" });
    const [, other] = within(list).getAllByRole("listitem");
    await user.click(
      within(other as HTMLElement).getByRole("button", { name: "End session" }),
    );
    const confirm = await screen.findByRole("alertdialog");
    await user.click(
      within(confirm).getByRole("button", { name: "End session" }),
    );

    expect(await screen.findByRole("alert")).toBeInTheDocument();
    expect(notifyError).not.toHaveBeenCalled();
  });
});

describe("signing in a new device", () => {
  it("opens the pairing dialog from the page's action", async () => {
    serve(() => Response.json(pairing()));
    const user = userEvent.setup();
    mount();

    const dialog = await openPairing(user);
    expect(
      firstOtpSlot("Pairing code shown on the new device", within(dialog)),
    ).toBeInTheDocument();
  });

  it("asks for the whole code before looking anything up", async () => {
    serve(() => Response.json(pairing()));
    const user = userEvent.setup();
    mount();

    const dialog = await enterCode(user, "");
    expect(
      await within(dialog).findByText(
        "Enter the full 8-character pairing code.",
      ),
    ).toBeInTheDocument();

    await user.type(
      firstOtpSlot("Pairing code shown on the new device", within(dialog)),
      "ABCD23",
    );
    await user.click(within(dialog).getByRole("button", { name: "Continue" }));
    expect(
      await within(dialog).findByText(
        "Enter the full 8-character pairing code.",
      ),
    ).toBeInTheDocument();
    expect(requests("/pair/lookup")).toHaveLength(0);
  });

  it("takes only the server's alphabet, in either case", async () => {
    serve(() => Response.json(pairing()));
    const user = userEvent.setup();
    mount();

    const dialog = await openPairing(user);
    const field = firstOtpSlot(
      "Pairing code shown on the new device",
      within(dialog),
    );
    // 0, 1, I, L and O are not in the alphabet, so those keystrokes are
    // refused and the rest stays as typed.
    await user.type(field, "ab0cd1");
    expect(otpValue("Pairing code shown on the new device")).toBe("abcd");
  });

  it("puts an unknown or expired code on the field and stays on the first step", async () => {
    serve(() =>
      Response.json(
        { code: "pairing_expired", requestId: "devices-test" },
        { status: 410 },
      ),
    );
    const user = userEvent.setup();
    mount();

    const dialog = await enterCode(user, "ABCD2345");
    await waitFor(() =>
      expect(
        firstOtpSlot("Pairing code shown on the new device", within(dialog)),
      ).toHaveAttribute("aria-invalid", "true"),
    );
    expect(
      within(dialog).getByRole("heading", { name: "Sign in a new device" }),
    ).toBeInTheDocument();
  });

  it("shows who is asking, where from, and what approving does", async () => {
    serve(() => Response.json(pairing({ initiatorIp: "192.0.2.1" })));
    const user = userEvent.setup();
    mount();

    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Let this device sign in?",
    });
    expect(within(dialog).getByText("Chrome · Windows")).toBeInTheDocument();
    expect(within(dialog).getByText("192.0.2.1")).toBeInTheDocument();
    expect(within(dialog).getByText("Same as this device")).toBeInTheDocument();
    expect(
      within(dialog).getByText(
        "Once approved, this device is signed in to your account.",
      ),
    ).toBeInTheDocument();
    // The code is what the reader just typed, so it is not shown back.
    expect(within(dialog).queryByText("ABCD-2345")).not.toBeInTheDocument();
    const lookup = requests("/pair/lookup")[0];
    expect(new URL(lookup?.url ?? "").searchParams.get("code")).toBe(
      "ABCD2345",
    );
  });

  it("stops offering the approval the moment the pairing expires", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    serve(() =>
      Response.json(
        pairing({ expiresAt: new Date(Date.now() + 5_000).toISOString() }),
      ),
    );
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    mount();

    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Let this device sign in?",
    });
    const approve = () =>
      within(dialog).getByRole("button", { name: "Approve" });
    expect(approve()).toBeEnabled();

    await vi.advanceTimersByTimeAsync(5_000);

    await waitFor(() => expect(approve()).toBeDisabled());
    // The reason is on the page, not only in a tooltip on the disabled button.
    expect(
      within(dialog).getByText(
        "This pairing code has expired. Start again on the new device.",
      ),
    ).toBeVisible();
  });

  it("approves, closes with a toast, and watches the list until the device arrives", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    serve(() => Response.json(pairing()));
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    mount();

    await screen.findByText("Safari · iOS");
    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Let this device sign in?",
    });
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

    // Not there yet: the list says it is waiting, and asks again.
    expect(
      await screen.findByText(
        "Waiting for the new device to finish signing in…",
      ),
    ).toBeInTheDocument();
    const before = requests("/me/sessions").length;
    await vi.advanceTimersByTimeAsync(3_000);
    await waitFor(() =>
      expect(requests("/me/sessions").length).toBe(before + 1),
    );

    // The new device has signed in: it shows up, and the asking stops.
    sessions = [...sessions, session(3, chromeWindows)];
    await vi.advanceTimersByTimeAsync(3_000);
    expect(await screen.findByText("Chrome · Windows")).toBeInTheDocument();
    expect(
      screen.queryByText("Waiting for the new device to finish signing in…"),
    ).not.toBeInTheDocument();
    const settled = requests("/me/sessions").length;
    await vi.advanceTimersByTimeAsync(9_000);
    expect(requests("/me/sessions")).toHaveLength(settled);
  });

  it("stops saying it is waiting when the pairing runs out unanswered", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true });
    serve(() =>
      Response.json(
        pairing({ expiresAt: new Date(Date.now() + 10_000).toISOString() }),
      ),
    );
    const user = userEvent.setup({ advanceTimers: vi.advanceTimersByTime });
    mount();

    await screen.findByText("Safari · iOS");
    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Let this device sign in?",
    });
    await user.click(within(dialog).getByRole("button", { name: "Approve" }));
    const waiting = "Waiting for the new device to finish signing in…";
    expect(await screen.findByText(waiting)).toBeInTheDocument();

    await vi.advanceTimersByTimeAsync(11_000);
    await waitFor(() =>
      expect(screen.queryByText(waiting)).not.toBeInTheDocument(),
    );
  });

  it("keeps the waiting line in a live region so it is announced", async () => {
    serve(() => Response.json(pairing()));
    const user = userEvent.setup();
    mount();

    await screen.findByText("Safari · iOS");
    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Let this device sign in?",
    });
    await user.click(within(dialog).getByRole("button", { name: "Approve" }));
    const line = await screen.findByText(
      "Waiting for the new device to finish signing in…",
    );
    expect(line.closest("[aria-live]")).toHaveAttribute("aria-live", "polite");
  });

  it("shows the address without a same-device note on another network", async () => {
    serve(() => Response.json(pairing({ initiatorIp: "198.51.100.7" })));
    const user = userEvent.setup();
    mount();

    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Let this device sign in?",
    });
    expect(within(dialog).getByText("198.51.100.7")).toBeInTheDocument();
    expect(within(dialog).queryByText(/Same as this device/)).toBeNull();
  });

  it("declines from the same dialog", async () => {
    serve(() => Response.json(pairing()));
    const user = userEvent.setup();
    mount();

    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Let this device sign in?",
    });
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

  it("offers to revoke a pairing this account already approved, not to approve it again", async () => {
    serve(() => Response.json(pairing({ alreadyBound: true })));
    const user = userEvent.setup();
    mount();

    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Device already approved",
    });
    expect(
      within(dialog).getByText(
        "You have already approved this pairing. The new device is finishing signing in.",
      ),
    ).toBeInTheDocument();
    expect(
      within(dialog).queryByRole("button", { name: "Approve" }),
    ).not.toBeInTheDocument();

    await user.click(
      within(dialog).getByRole("button", { name: "Revoke approval" }),
    );
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    expect(requests("/pair/cancel")).toHaveLength(1);
    expect(notifySuccess).toHaveBeenCalledWith(
      expect.objectContaining({ id: "success.device.approval-revoked" }),
    );
  });

  it("leaves a failed approval to the toast and draws no error in the dialog", async () => {
    serve(
      () => Response.json(pairing()),
      (path) =>
        path.endsWith("/pair/approve")
          ? Response.json(
              { code: "internal_error", requestId: "devices-test" },
              { status: 500 },
            )
          : undefined,
    );
    const user = userEvent.setup();
    mount();

    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Let this device sign in?",
    });
    await user.click(within(dialog).getByRole("button", { name: "Approve" }));

    await waitFor(() => expect(notifyError).toHaveBeenCalledTimes(1));
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Approve" }),
    ).toBeEnabled();
  });

  it("starts again at the code the next time it opens", async () => {
    serve(() => Response.json(pairing()));
    const user = userEvent.setup();
    mount();

    await enterCode(user, "ABCD2345");
    const dialog = await screen.findByRole("dialog", {
      name: "Let this device sign in?",
    });
    await user.click(within(dialog).getByRole("button", { name: "Decline" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );

    const again = await openPairing(user);
    expect(
      firstOtpSlot("Pairing code shown on the new device", within(again)),
    ).toHaveValue("");
  });

  it("opens with the code a pairing link brought, and looks it up on Continue", async () => {
    serve(() => Response.json(pairing()));
    const user = userEvent.setup();
    mount("ABCD2345");

    const dialog = await screen.findByRole("dialog", {
      name: "Sign in a new device",
    });
    expect(
      otpValue("Pairing code shown on the new device", within(dialog)),
    ).toBe("ABCD2345");
    expect(requests("/pair/lookup")).toHaveLength(0);

    await user.click(within(dialog).getByRole("button", { name: "Continue" }));
    expect(
      await screen.findByRole("dialog", { name: "Let this device sign in?" }),
    ).toBeInTheDocument();
    const [lookup] = requests("/pair/lookup");
    expect(new URL(lookup?.url ?? "").searchParams.get("code")).toBe(
      "ABCD2345",
    );
  });

  it("starts from an empty code when opened again after a pairing link", async () => {
    serve(() => Response.json(pairing()));
    const user = userEvent.setup();
    mount("ABCD2345");

    const first = await screen.findByRole("dialog", {
      name: "Sign in a new device",
    });
    await user.click(within(first).getByRole("button", { name: "Cancel" }));
    await waitFor(() =>
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
    );
    const again = await openPairing(user);
    expect(
      firstOtpSlot("Pairing code shown on the new device", within(again)),
    ).toHaveValue("");
  });
});

describe("the pairing link's code", () => {
  const validate = (search: Record<string, unknown>) =>
    devicesSearch.parse(search);

  it("keeps a whole code in the server's alphabet, in either case", () => {
    expect(validate({ code: "K7QM4XTB" })).toEqual({ code: "K7QM4XTB" });
    expect(validate({ code: "k7qm4xtb" })).toEqual({ code: "k7qm4xtb" });
  });

  it.each([
    ["a short code", "K7QM4XT"],
    ["a long one", "K7QM4XTBA"],
    ["a formatted one", "K7QM-4XTB"],
    ["one with a letter outside the alphabet", "K7QM4XTO"],
    ["a repeated one", ["K7QM4XTB", "K7QM4XTB"]],
  ])("drops %s", (_, code) => {
    expect(validate({ code }).code).toBeUndefined();
  });

  it("opens the page without one", () => {
    expect(validate({}).code).toBeUndefined();
  });
});
