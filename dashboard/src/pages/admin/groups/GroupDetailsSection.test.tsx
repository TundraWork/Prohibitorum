import type { MessageDescriptor } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { AppGroupView } from "@/api/raw-admin-paths";
import { createQueryClient } from "@/app/query-client";
import { i18n } from "@/i18n";
import { GroupDetailsSection } from "@/pages/admin/groups/GroupDetailsSection";
import { apiError } from "@/test/app";
import { groupFixture, ruleGroupFixture } from "@/test/group";

const fetchBoundary = vi.fn<(request: Request) => Promise<Response>>();
const notifyError = vi.fn<(error: unknown) => void>();
let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  fetchBoundary.mockReset();
  notifyError.mockReset();
  vi.stubGlobal("fetch", fetchBoundary);
  queryClient = createQueryClient(
    notifyError,
    vi.fn<(message: MessageDescriptor) => void>(),
  );
});

afterEach(() => {
  queryClient.clear();
  vi.unstubAllGlobals();
});

function puts() {
  return fetchBoundary.mock.calls
    .map(([request]) => request)
    .filter((request) => request.method === "PUT");
}

function mount(
  group: AppGroupView,
  extra?: (request: Request) => Response | Promise<Response> | undefined,
) {
  fetchBoundary.mockImplementation(async (request) => {
    const canned = extra?.(request);
    if (canned) return canned;
    if (request.method === "PUT") return Response.json(group);
    return new Response(null, { status: 204 });
  });
  return render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <GroupDetailsSection group={group} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

const nameField = () => screen.getByRole("textbox", { name: "Name" });
const slugField = () => screen.getByRole("textbox", { name: "Identifier" });
const descriptionField = () =>
  screen.getByRole("textbox", { name: "Description" });
const sendSwitch = () =>
  screen.getByRole("switch", { name: "Send to applications" });
const turnOff =
  "Applications that selected Staff stop receiving staff at the next sign-in.";

describe("the details section", () => {
  it("shows the stored values, the kind as a fact, and no footer until something changes", () => {
    mount(groupFixture());

    expect(
      screen.getByRole("heading", { level: 2, name: "Details" }),
    ).toBeVisible();
    expect(nameField()).toHaveValue("Staff");
    expect(slugField()).toHaveValue("staff");
    expect(descriptionField()).toHaveValue("Everyone employed here");
    expect(sendSwitch()).toBeChecked();
    expect(screen.getByRole("textbox", { name: "How it fills" })).toHaveValue(
      "Manual",
    );
    expect(
      screen.getByRole("textbox", { name: "How it fills" }),
    ).toHaveAttribute("readonly");
    expect(
      screen.getByText(
        "A group's kind is set when it is created and cannot be changed.",
      ),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("says what sending controls, and what it does not", () => {
    mount(groupFixture());

    expect(sendSwitch()).toHaveAccessibleDescription(
      /groups claim, the SAML groups attribute and the Remote-Groups header/,
    );
    expect(sendSwitch()).toHaveAccessibleDescription(/does not limit/);
  });

  it("reads a rule group's kind as Rule", () => {
    mount(ruleGroupFixture());
    expect(screen.getByRole("textbox", { name: "How it fills" })).toHaveValue(
      "Rule",
    );
  });

  it("saves the full body, without a rule, and stays on the page", async () => {
    const user = userEvent.setup();
    mount(ruleGroupFixture({ description: "Who uses one" }));

    await user.clear(nameField());
    await user.type(nameField(), "Passkey people");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(new URL(puts()[0]?.url ?? "").pathname).toBe(
      "/api/prohibitorum/groups/8",
    );
    expect(await puts()[0]?.json()).toEqual({
      slug: "passkey-users",
      displayName: "Passkey people",
      description: "Who uses one",
      exposedToDownstream: true,
    });
    expect(screen.queryByRole("alertdialog")).toBeNull();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("trims what it sends and sends an emptied description as empty", async () => {
    const user = userEvent.setup();
    mount(groupFixture());

    await user.clear(descriptionField());
    await user.clear(slugField());
    await user.type(slugField(), "  team  ");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      slug: "team",
      displayName: "Staff",
      description: "",
      exposedToDownstream: true,
    });
  });

  it("puts the slug conflict on the identifier field, reported once", async () => {
    const user = userEvent.setup();
    mount(groupFixture(), (request) =>
      request.method === "PUT"
        ? apiError("group_slug_conflict", 409)
        : undefined,
    );

    await user.clear(slugField());
    await user.type(slugField(), "taken");
    await user.click(screen.getByRole("button", { name: "Save" }));

    expect(
      await screen.findByText(
        "Another user group already uses this identifier.",
      ),
    ).toBeVisible();
    expect(slugField()).toBeInvalid();
    expect(
      screen.getAllByText("Another user group already uses this identifier."),
    ).toHaveLength(1);
    expect(screen.queryByText("Unable to submit")).toBeNull();
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("reports another refusal once, on the form", async () => {
    const user = userEvent.setup();
    mount(groupFixture(), (request) =>
      request.method === "PUT" ? apiError("internal_error", 500) : undefined,
    );

    await user.type(descriptionField(), " more");
    await user.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });

  it("asks for a name and a well-formed identifier before sending anything", async () => {
    const user = userEvent.setup();
    mount(groupFixture());

    await user.clear(nameField());
    expect(await screen.findByText("Give the group a name.")).toBeVisible();
    await user.type(nameField(), "Staff");
    await user.clear(slugField());
    expect(
      await screen.findByText("Give the group an identifier."),
    ).toBeVisible();
    await user.type(slugField(), "Not Valid");
    expect(
      await screen.findByText(
        "Use lowercase letters, digits and hyphens, up to 64 characters.",
      ),
    ).toBeVisible();

    await user.click(screen.getByRole("button", { name: "Save" }));
    expect(puts()).toHaveLength(0);
  });

  it("puts the stored values back with Discard", async () => {
    const user = userEvent.setup();
    mount(groupFixture());

    await user.type(nameField(), " edited");
    await user.click(screen.getByRole("button", { name: "Discard" }));

    expect(nameField()).toHaveValue("Staff");
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
    expect(puts()).toHaveLength(0);
  });
});

describe("turning sending off", () => {
  it("states the consequence and names the action in the footer", async () => {
    const user = userEvent.setup();
    mount(groupFixture());

    await user.click(sendSwitch());

    expect(screen.getByText(turnOff)).toBeVisible();
    expect(
      screen.getByRole("button", { name: "Stop sending and save" }),
    ).toBeVisible();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("says nothing about consequences when sending is turned on", async () => {
    const user = userEvent.setup();
    mount(groupFixture({ exposedToDownstream: false }));

    await user.click(sendSwitch());

    expect(screen.queryByText(/stop receiving/)).toBeNull();
    expect(screen.getByRole("button", { name: "Save" })).toBeVisible();
  });

  it("drops the consequence when the switch is put back", async () => {
    const user = userEvent.setup();
    mount(groupFixture());

    await user.click(sendSwitch());
    await user.click(sendSwitch());

    expect(screen.queryByText(turnOff)).toBeNull();
    expect(screen.queryByRole("button", { name: "Save" })).toBeNull();
  });

  it("confirms before it writes, then sends the full body", async () => {
    const user = userEvent.setup();
    mount(groupFixture());

    await user.click(sendSwitch());
    await user.click(
      screen.getByRole("button", { name: "Stop sending and save" }),
    );
    const dialog = await screen.findByRole("alertdialog", {
      name: "Stop sending Staff to applications?",
    });
    expect(puts()).toHaveLength(0);
    await user.click(
      within(dialog).getByRole("button", { name: "Stop sending" }),
    );

    await waitFor(() => expect(puts()).toHaveLength(1));
    expect(await puts()[0]?.json()).toEqual({
      slug: "staff",
      displayName: "Staff",
      description: "Everyone employed here",
      exposedToDownstream: false,
    });
  });

  it("keeps the choice in the form, unsent, when the confirmation is cancelled", async () => {
    const user = userEvent.setup();
    mount(groupFixture());

    await user.click(sendSwitch());
    await user.click(
      screen.getByRole("button", { name: "Stop sending and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(within(dialog).getByRole("button", { name: "Cancel" }));

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(sendSwitch()).not.toBeChecked();
    expect(puts()).toHaveLength(0);
  });

  it("keeps the confirmation open while the write is in flight", async () => {
    const user = userEvent.setup();
    let release: (response: Response) => void = () => {};
    mount(groupFixture(), (request) =>
      request.method === "PUT"
        ? new Promise<Response>((resolve) => {
            release = resolve;
          })
        : undefined,
    );

    await user.click(sendSwitch());
    await user.click(
      screen.getByRole("button", { name: "Stop sending and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Stop sending" }),
    );
    await waitFor(() => expect(puts()).toHaveLength(1));
    await user.keyboard("{Escape}");

    expect(screen.getByRole("alertdialog")).toBeVisible();
    release(Response.json(groupFixture({ exposedToDownstream: false })));
  });

  it("closes the confirmation and reports a refusal once, on the form", async () => {
    const user = userEvent.setup();
    mount(groupFixture(), (request) =>
      request.method === "PUT" ? apiError("internal_error", 500) : undefined,
    );

    await user.click(sendSwitch());
    await user.click(
      screen.getByRole("button", { name: "Stop sending and save" }),
    );
    const dialog = await screen.findByRole("alertdialog");
    await user.click(
      within(dialog).getByRole("button", { name: "Stop sending" }),
    );

    await waitFor(() =>
      expect(screen.queryByRole("alertdialog")).not.toBeInTheDocument(),
    );
    expect(await screen.findAllByRole("alert")).toHaveLength(1);
    expect(notifyError).not.toHaveBeenCalled();
  });
});
