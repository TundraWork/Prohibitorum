import { I18nProvider } from "@lingui/react";
import {
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  type RouterHistory,
  RouterProvider,
} from "@tanstack/react-router";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { type ReactNode, type RefObject, useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  SecretReveal,
  SecretRevealDialog,
  SecretRevealStep,
} from "@/components/custom/SecretReveal";
import {
  accessTokenCopy,
  invitationLinkCopy,
  recoveryCodesCopy,
  type SecretRevealCopy,
} from "@/components/custom/secret-reveal-copy";
import { Dialog, DialogPopup } from "@/components/ui/dialog";
import { i18n } from "@/i18n";
import { clientSecretCopy } from "@/pages/admin/oidc-applications/secret-copy";

const codes = "aaaa-bbbb\ncccc-dddd";
const link = "https://id.example.test/enroll?token=abc&for=alice";
const codesLeave =
  "These recovery codes cannot be shown again after this is closed.";

type User = ReturnType<typeof setupUser>;

/**
 * `userEvent` installs the clipboard jsdom lacks. It runs without delays so
 * its own waits never touch faked timers.
 */
function setupUser() {
  return userEvent.setup({ delay: null });
}

let history: RouterHistory;

beforeEach(() => {
  i18n.activate("en");
  history = createBrowserHistory();
  // The clipboard stub spies attach to appears with the first `setup()`, so a
  // shuffled run must not rely on another test having gone first.
  setupUser();
});

afterEach(() => {
  history.destroy();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

/**
 * Renders `ui` as the page at `/`, beside an `/elsewhere` page to navigate
 * to: the leaving guard is the router's, so every form needs one mounted.
 */
async function renderRouted(ui: () => ReactNode) {
  const root = createRootRoute({ component: Outlet });
  const router = createRouter({
    routeTree: root.addChildren([
      createRoute({ getParentRoute: () => root, path: "/", component: ui }),
      createRoute({
        getParentRoute: () => root,
        path: "/elsewhere",
        component: () => <h1>Elsewhere</h1>,
      }),
    ]),
    history,
  });
  render(
    <I18nProvider i18n={i18n}>
      <RouterProvider router={router} />
    </I18nProvider>,
  );
  await waitFor(() =>
    expect(document.querySelector('[data-slot="secret-well"]')).not.toBeNull(),
  );
  return router;
}

interface MountOptions {
  text?: string;
  copy?: SecretRevealCopy;
  onContinue?: () => Promise<void>;
}

async function mount({
  text = codes,
  copy = recoveryCodesCopy,
  onContinue = vi.fn(async () => undefined),
}: MountOptions = {}) {
  const user = setupUser();
  const router = await renderRouted(() => (
    <SecretReveal
      text={text}
      filename="codes.txt"
      copy={copy}
      onContinue={onContinue}
    />
  ));
  return { user, onContinue, router };
}

async function mountDialog({
  text = codes,
  copy = recoveryCodesCopy,
  onContinue = vi.fn(async () => undefined),
}: MountOptions = {}) {
  const user = setupUser();
  const router = await renderRouted(() => (
    <SecretRevealDialog
      text={text}
      filename="codes.txt"
      copy={copy}
      onContinue={onContinue}
    />
  ));
  const dialog = await screen.findByRole("dialog", { name: /./ });
  return { user, onContinue, router, dialog };
}

const stepProceed: { current: RefObject<(() => void) | null> | null } = {
  current: null,
};

/** A caller's dialog that routes its own Escape and close button through the step. */
function StepHost({ onContinue }: { onContinue: () => Promise<void> }) {
  const proceedRef = useRef<(() => void) | null>(null);
  stepProceed.current = proceedRef;
  return (
    <Dialog
      open
      disablePointerDismissal
      onOpenChange={(open) => {
        if (!open) proceedRef.current?.();
      }}
    >
      <DialogPopup>
        <SecretRevealStep
          text={codes}
          filename="codes.txt"
          copy={recoveryCodesCopy}
          onContinue={onContinue}
          progress={<p>Step 2 of 2</p>}
          proceedRef={proceedRef}
        />
      </DialogPopup>
    </Dialog>
  );
}

async function mountStep(onContinue = vi.fn(async () => undefined)) {
  const user = setupUser();
  await renderRouted(() => <StepHost onContinue={onContinue} />);
  const dialog = await screen.findByRole("dialog", { name: /./ });
  return { user, onContinue, dialog };
}

/**
 * The well captioned with `label`. jsdom's accessibility tree does not name a
 * figure from its figcaption, so the caption is matched directly.
 */
function wellNamed(label: string, scope: HTMLElement = document.body) {
  const figure = within(scope).getByRole("figure");
  expect(figure.tagName).toBe("FIGURE");
  expect(figure.querySelector(":scope > figcaption")).toHaveTextContent(
    new RegExp(`^${label}$`),
  );
  return figure;
}

function well() {
  return screen.getByRole("figure");
}

function mockClipboard() {
  return vi
    .spyOn(navigator.clipboard, "writeText")
    .mockResolvedValue(undefined);
}

function mockDownload() {
  let blob: Blob | undefined;
  const create = vi
    .spyOn(URL, "createObjectURL")
    .mockImplementation((object) => {
      blob = object as Blob;
      return "blob:codes";
    });
  vi.spyOn(URL, "revokeObjectURL").mockImplementation(() => undefined);
  let filename: string | undefined;
  const click = vi
    .spyOn(HTMLAnchorElement.prototype, "click")
    .mockImplementation(function (this: HTMLAnchorElement) {
      filename = this.download;
    });
  return { create, click, blob: () => blob, filename: () => filename };
}

/** The leave confirmation, once it opens. */
function findLeaveConfirm() {
  return screen.findByRole("alertdialog", { name: "Close without copying?" });
}

async function cancelLeave(user: User) {
  const confirm = await findLeaveConfirm();
  await user.click(within(confirm).getByRole("button", { name: "Cancel" }));
  await waitFor(() =>
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
  );
}

async function closeAnyway(user: User) {
  const confirm = await findLeaveConfirm();
  await user.click(
    within(confirm).getByRole("button", { name: "Close anyway" }),
  );
}

function beforeUnloadPrevented() {
  const event = new Event("beforeunload", { cancelable: true });
  window.dispatchEvent(event);
  return event.defaultPrevented;
}

/**
 * Copies through the button and waits until the reveal counts it: the label
 * changes first, and the guard follows once the reveal has rendered again.
 */
async function copyByButton(user: User, label = "Copy codes") {
  const write = mockClipboard();
  await user.click(screen.getByRole("button", { name: label }));
  await screen.findByRole("button", { name: "Copied" });
  await waitFor(() => expect(beforeUnloadPrevented()).toBe(false));
  return write;
}

describe("SecretReveal", () => {
  it("shows recovery codes once, one per item in a well captioned with the label", async () => {
    await mount();
    const figure = wellNamed("New recovery codes");
    expect(
      within(figure)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["aaaa-bbbb", "cccc-dddd"]);
    expect(
      screen.getByRole("heading", { name: "Save your new recovery codes" }),
    ).toBeVisible();
    expect(
      screen.getByText(
        "These codes are shown only once. Save them somewhere safe before you continue. Your old recovery codes no longer work.",
      ).tagName,
    ).toBe("P");
    // The once sentence is a plain paragraph, not a warning box, and there is
    // no confirmation to tick and no field to read the value from.
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  });

  it("shows a single-line value as one paragraph whose text is the value", async () => {
    await mount({ text: link, copy: invitationLinkCopy });
    const figure = wellNamed("Registration link");
    expect(within(figure).queryByRole("list")).not.toBeInTheDocument();
    const value = figure.querySelector(":scope > p");
    // The break points are empty elements, so the text is the link unchanged.
    expect(value?.textContent).toBe(link);
    expect(value?.querySelectorAll("wbr").length).toBeGreaterThan(0);
  });

  it("keeps continue enabled from the start", async () => {
    await mount();
    expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();
  });

  it("copies from an outline button, which then says Copied with a check, and a status announces it, for two seconds", async () => {
    vi.useFakeTimers({
      toFake: ["setTimeout", "clearTimeout"],
      shouldAdvanceTime: true,
    });
    const { user } = await mount();
    const write = mockClipboard();
    const button = screen.getByRole("button", { name: "Copy codes" });
    expect(button).toHaveAttribute("data-variant", "outline");
    expect(screen.getByRole("status")).toBeEmptyDOMElement();

    await user.click(button);

    expect(write).toHaveBeenCalledWith(codes);
    expect(await screen.findByRole("button", { name: "Copied" })).toBe(button);
    const copied = within(button).getByText("Copied");
    expect(copied).toBeVisible();
    expect(copied.querySelector("svg.lucide-check")).not.toBeNull();
    expect(within(button).getByText("Copy codes")).not.toBeVisible();
    expect(screen.getByRole("status")).toHaveTextContent("Copied");

    act(() => {
      vi.advanceTimersByTime(2000);
    });

    expect(screen.getByRole("button", { name: "Copy codes" })).toBe(button);
    expect(within(button).getByText("Copy codes")).toBeVisible();
    expect(within(button).getByText("Copied")).not.toBeVisible();
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
  });

  it("warns when the clipboard refuses the copy, and says nothing was copied", async () => {
    const { user } = await mount();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
      new DOMException("Denied", "NotAllowedError"),
    );

    await user.click(screen.getByRole("button", { name: "Copy codes" }));

    expect(await screen.findByRole("alert")).toHaveTextContent(
      "Could not copy the codes. Select the codes above and copy them manually, or download them.",
    );
    expect(screen.getByRole("button", { name: "Copy codes" })).toBeVisible();
    expect(screen.getByRole("status")).toBeEmptyDOMElement();
  });

  it("downloads recovery codes as a text file", async () => {
    const { user } = await mount();
    const download = mockDownload();

    await user.click(screen.getByRole("button", { name: "Download codes" }));

    expect(download.create).toHaveBeenCalledTimes(1);
    expect(download.click).toHaveBeenCalledTimes(1);
    expect(download.filename()).toBe("codes.txt");
    expect(await download.blob()?.text()).toBe(`${codes}\n`);
  });

  it.each([
    ["a registration link", link, invitationLinkCopy, "Copy link"],
    ["a client secret", "s3cr3t", clientSecretCopy, "Copy secret"],
    ["an access token", "prohibitorum_pat_abcd", accessTokenCopy, "Copy token"],
  ])("offers no download for %s", async (_, text, copy, copyLabel) => {
    await mount({ text, copy });
    expect(within(well()).getAllByRole("button")).toHaveLength(1);
    expect(
      within(well()).getByRole("button", { name: copyLabel }),
    ).toBeVisible();
    expect(
      screen.queryByRole("button", { name: /download/i }),
    ).not.toBeInTheDocument();
  });

  it("asks before continuing while the secret was never taken; Cancel returns to it", async () => {
    const { user, onContinue } = await mount();

    await user.click(screen.getByRole("button", { name: "Continue" }));

    const confirm = await findLeaveConfirm();
    expect(confirm).toHaveTextContent(codesLeave);
    expect(
      within(confirm).getByRole("button", { name: "Close anyway" }),
    ).toBeVisible();
    await cancelLeave(user);

    expect(onContinue).not.toHaveBeenCalled();
    expect(wellNamed("New recovery codes")).toBeVisible();
    expect(screen.getByRole("button", { name: "Continue" })).toBeEnabled();
  });

  it("continues on Close anyway", async () => {
    const { user, onContinue } = await mount();

    await user.click(screen.getByRole("button", { name: "Continue" }));
    await closeAnyway(user);

    expect(onContinue).toHaveBeenCalledTimes(1);
  });

  it("continues at once after a copy", async () => {
    const { user, onContinue } = await mount();

    await copyByButton(user);
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(onContinue).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("continues at once after a download", async () => {
    const { user, onContinue } = await mount();
    mockDownload();

    await user.click(screen.getByRole("button", { name: "Download codes" }));
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(onContinue).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("does not count a refused copy as taken", async () => {
    const { user, onContinue } = await mount();
    vi.spyOn(navigator.clipboard, "writeText").mockRejectedValue(
      new DOMException("Denied", "NotAllowedError"),
    );

    await user.click(screen.getByRole("button", { name: "Copy codes" }));
    await screen.findByRole("alert");
    await user.click(screen.getByRole("button", { name: "Continue" }));

    expect(await findLeaveConfirm()).toBeVisible();
    expect(onContinue).not.toHaveBeenCalled();
  });

  it("counts a copy by hand from the codes as taken", async () => {
    const { user, onContinue } = await mount();

    fireEvent.copy(within(well()).getByRole("list"));

    expect(await screen.findByRole("status")).toHaveTextContent("Copied");
    await user.click(screen.getByRole("button", { name: "Continue" }));
    expect(onContinue).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("counts a copy by hand from a single-line value as taken", async () => {
    const { user, onContinue } = await mount({
      text: link,
      copy: invitationLinkCopy,
    });

    const value = well().querySelector(":scope > p");
    if (value === null) throw new Error("no value in the well");
    fireEvent.copy(value);

    await user.click(screen.getByRole("button", { name: "Done" }));
    expect(onContinue).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });
});

/** The three ways out of the dialog besides a press outside. */
const ways: [string, (user: User, dialog: HTMLElement) => Promise<void>][] = [
  [
    "Continue",
    (user, dialog) =>
      user.click(within(dialog).getByRole("button", { name: "Continue" })),
  ],
  [
    "the close button",
    (user, dialog) =>
      user.click(within(dialog).getByRole("button", { name: "Close" })),
  ],
  ["Escape", (user) => user.keyboard("{Escape}")],
];

describe("SecretRevealDialog", () => {
  it("is a titled dialog described by the once sentence, holding the secret in its well", async () => {
    const { dialog } = await mountDialog();

    expect(dialog).toHaveAccessibleName("Save your new recovery codes");
    expect(dialog).toHaveAccessibleDescription(
      "These codes are shown only once. Save them somewhere safe before you continue. Your old recovery codes no longer work.",
    );
    const figure = wellNamed("New recovery codes", dialog);
    expect(
      within(figure)
        .getAllByRole("listitem")
        .map((item) => item.textContent),
    ).toEqual(["aaaa-bbbb", "cccc-dddd"]);
    expect(within(dialog).queryByRole("alert")).not.toBeInTheDocument();
    expect(within(dialog).queryByRole("checkbox")).not.toBeInTheDocument();
    expect(
      within(dialog).getByRole("button", { name: "Continue" }),
    ).toBeEnabled();
  });

  it("shows its close button even before the secret is taken", async () => {
    const { dialog } = await mountDialog();
    expect(within(dialog).getByRole("button", { name: "Close" })).toBeVisible();
  });

  it("never closes or asks on a press outside", async () => {
    const { user, onContinue } = await mountDialog();

    await user.click(document.body);

    expect(
      screen.getByRole("dialog", { name: "Save your new recovery codes" }),
    ).toBeVisible();
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
    expect(onContinue).not.toHaveBeenCalled();
  });

  it.each(ways)(
    "asks before leaving by %s while the secret was never taken; Cancel keeps it open",
    async (_, leave) => {
      const { user, onContinue, dialog } = await mountDialog();

      await leave(user, dialog);

      expect(await findLeaveConfirm()).toHaveTextContent(codesLeave);
      await cancelLeave(user);
      expect(
        screen.getByRole("dialog", { name: "Save your new recovery codes" }),
      ).toBeVisible();
      expect(onContinue).not.toHaveBeenCalled();
    },
  );

  it.each(ways)("leaves by %s on Close anyway", async (_, leave) => {
    const { user, onContinue, dialog } = await mountDialog();

    await leave(user, dialog);
    await closeAnyway(user);

    expect(onContinue).toHaveBeenCalledTimes(1);
  });

  it.each(ways)("leaves by %s at once after a copy", async (_, leave) => {
    const { user, onContinue, dialog } = await mountDialog();

    await copyByButton(user);
    await leave(user, dialog);

    await waitFor(() => expect(onContinue).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("names the lost link in the confirmation for a link, and offers no download", async () => {
    const { user, dialog } = await mountDialog({
      text: link,
      copy: invitationLinkCopy,
    });
    expect(dialog).toHaveAccessibleName("Copy the registration link");
    expect(
      within(dialog).queryByRole("button", { name: /download/i }),
    ).not.toBeInTheDocument();

    await user.click(within(dialog).getByRole("button", { name: "Done" }));

    expect(await findLeaveConfirm()).toHaveTextContent(
      "This link cannot be shown again after this is closed.",
    );
  });
});

describe("SecretRevealStep", () => {
  it("draws the caller's progress over the title and the once sentence as the description", async () => {
    const { dialog } = await mountStep();
    expect(dialog).toHaveAccessibleName("Save your new recovery codes");
    expect(within(dialog).getByText("Step 2 of 2")).toBeVisible();
    expect(dialog).toHaveAccessibleDescription(
      "These codes are shown only once. Save them somewhere safe before you continue. Your old recovery codes no longer work.",
    );
    expect(wellNamed("New recovery codes", dialog)).toBeVisible();
    expect(
      within(dialog).getByRole("button", { name: "Continue" }),
    ).toBeEnabled();
  });

  it("hands the caller a proceed that asks first while the codes were never taken", async () => {
    const { user, onContinue } = await mountStep();
    expect(stepProceed.current?.current).toBeTypeOf("function");

    // The caller's Escape goes through the step's proceed...
    await user.keyboard("{Escape}");
    expect(await findLeaveConfirm()).toHaveTextContent(codesLeave);
    await cancelLeave(user);
    expect(screen.getByRole("dialog", { name: /./ })).toBeVisible();
    expect(onContinue).not.toHaveBeenCalled();

    // ...and so does its close button, which goes on with Close anyway.
    await user.click(screen.getByRole("button", { name: "Close" }));
    await closeAnyway(user);
    expect(onContinue).toHaveBeenCalledTimes(1);
  });

  it("continues at once through the caller's Escape after a download", async () => {
    const { user, onContinue, dialog } = await mountStep();
    mockDownload();

    await user.click(
      within(dialog).getByRole("button", { name: "Download codes" }),
    );
    await user.keyboard("{Escape}");

    await waitFor(() => expect(onContinue).toHaveBeenCalledTimes(1));
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });
});

describe("SecretReveal leaving guard", () => {
  /** A navigation away, through the history every route change goes through. */
  function navigateAway() {
    // A held navigation settles only once the confirmation is answered.
    return act(async () => {
      history.push("/elsewhere");
    });
  }

  it("holds a navigation away from a secret never taken in the same confirmation; Cancel stays", async () => {
    const { user, onContinue, router } = await mount();
    const native = vi.spyOn(window, "confirm");

    await navigateAway();

    expect(await findLeaveConfirm()).toHaveTextContent(codesLeave);
    expect(router.state.location.pathname).toBe("/");
    await cancelLeave(user);

    expect(router.state.location.pathname).toBe("/");
    expect(wellNamed("New recovery codes")).toBeVisible();
    expect(onContinue).not.toHaveBeenCalled();
    expect(native).not.toHaveBeenCalled();

    // A later navigation is held again rather than let through.
    await navigateAway();
    expect(await findLeaveConfirm()).toBeVisible();
    expect(router.state.location.pathname).toBe("/");
  });

  it("lets a held navigation through on Close anyway, without continuing", async () => {
    const { user, onContinue, router } = await mount();

    await navigateAway();
    await closeAnyway(user);

    expect(
      await screen.findByRole("heading", { name: "Elsewhere" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/elsewhere");
    expect(onContinue).not.toHaveBeenCalled();
  });

  it("holds a navigation away from the dialog form too", async () => {
    const { onContinue, router } = await mountDialog({
      text: link,
      copy: invitationLinkCopy,
    });

    await navigateAway();

    expect(await findLeaveConfirm()).toHaveTextContent(
      "This link cannot be shown again after this is closed.",
    );
    expect(router.state.location.pathname).toBe("/");
    expect(onContinue).not.toHaveBeenCalled();
  });

  it("lets a navigation through at once after the secret was copied", async () => {
    const { user, router } = await mount();

    await copyByButton(user);
    await navigateAway();

    expect(
      await screen.findByRole("heading", { name: "Elsewhere" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/elsewhere");
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });

  it("leaves closing the tab to the browser's prompt until the secret is taken", async () => {
    await mount();
    expect(beforeUnloadPrevented()).toBe(true);

    fireEvent.copy(within(well()).getByRole("list"));

    await waitFor(() => expect(beforeUnloadPrevented()).toBe(false));
  });

  it("stops guarding once the reader continues", async () => {
    const { user, onContinue, router } = await mount();

    await user.click(screen.getByRole("button", { name: "Continue" }));
    await closeAnyway(user);
    expect(onContinue).toHaveBeenCalledTimes(1);

    expect(beforeUnloadPrevented()).toBe(false);
    await navigateAway();
    expect(
      await screen.findByRole("heading", { name: "Elsewhere" }),
    ).toBeVisible();
    expect(router.state.location.pathname).toBe("/elsewhere");
    expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument();
  });
});
