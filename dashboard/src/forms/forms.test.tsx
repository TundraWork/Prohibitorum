import { I18nProvider } from "@lingui/react";
import { type QueryClient, QueryClientProvider } from "@tanstack/react-query";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import { createQueryClient } from "@/app/query-client";
import {
  AppNotifications,
  notificationQueue,
  notifyError,
} from "@/components/custom/AppNotifications";
import { ServerFormError } from "@/forms/server-errors";
import { useAppForm } from "@/forms/use-app-form";
import { i18n } from "@/i18n";
import DevForms from "@/pages/DevForms";

let queryClient: QueryClient;

beforeEach(() => {
  i18n.activate("en");
  queryClient = createQueryClient(notifyError);
});

afterEach(() => {
  queryClient.clear();
  notificationQueue.clear();
  vi.unstubAllGlobals();
});

function mountForms() {
  render(
    <I18nProvider i18n={i18n}>
      <QueryClientProvider client={queryClient}>
        <DevForms />
        <AppNotifications />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

function publicError(code: string, location?: string) {
  return Response.json(
    {
      code,
      details: location
        ? { location, reason: "Untrusted response text" }
        : undefined,
      requestId: "test-form-request",
    },
    { status: 422 },
  );
}

describe("mutation form feedback", () => {
  it("freezes a submission, reports the field and toast, then keeps editable values for retry", async () => {
    let resolvePending!: (response: Response) => void;
    const pending = new Promise<Response>((resolve) => {
      resolvePending = resolve;
    });
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockReturnValueOnce(pending)
      .mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetch);
    const user = userEvent.setup();
    mountForms();
    const form = screen.getByRole("form", {
      name: "Credential nickname request",
    });
    const id = within(form).getByRole("textbox", { name: "Credential ID" });
    const nickname = within(form).getByRole("textbox", { name: "Nickname" });
    await user.type(id, "7");
    await user.type(nickname, "Test nickname");
    fireEvent.submit(form);
    fireEvent.submit(form);
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(1));
    expect(id).toBeDisabled();
    expect(nickname).toBeDisabled();
    expect(within(form).getByRole("button")).toHaveAttribute(
      "aria-disabled",
      "true",
    );
    expect(form).toHaveAttribute("aria-busy", "true");
    await act(async () =>
      resolvePending(publicError("validation_failed", "body.nickname")),
    );
    await waitFor(() => expect(nickname).toHaveFocus());
    expect(nickname).toBeEnabled();
    expect(nickname).toHaveValue("Test nickname");
    expect(nickname).toHaveAttribute("aria-invalid", "true");
    expect(nickname).toHaveAccessibleDescription();
    expect(
      screen.getAllByRole("button", { name: "Close notification" }),
    ).toHaveLength(1);
    expect(
      screen.queryByText("Untrusted response text"),
    ).not.toBeInTheDocument();
    await user.type(nickname, " edited");
    expect(nickname).not.toHaveAttribute("aria-invalid", "true");
    await user.click(within(form).getByRole("button"));
    await waitFor(() => expect(fetch).toHaveBeenCalledTimes(2));
    await waitFor(() =>
      expect(
        screen.getByText("Nickname saved. Your input has been kept."),
      ).toBeVisible(),
    );
    expect(nickname).toHaveValue("Test nickname edited");
  });

  it("focuses unknown-location summaries and clears them before a client-invalid retry without another request", async () => {
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValue(publicError("validation_failed", "__proto__"));
    vi.stubGlobal("fetch", fetch);
    const user = userEvent.setup();
    mountForms();
    const form = screen.getByRole("form", {
      name: "Credential nickname request",
    });
    const id = within(form).getByRole("textbox", { name: "Credential ID" });
    const nickname = within(form).getByRole("textbox", { name: "Nickname" });
    await user.type(id, "8");
    await user.type(nickname, "Preserved");
    await user.click(within(form).getByRole("button"));
    const summary = await within(form).findByRole("alert");
    await waitFor(() => expect(summary).toHaveFocus());
    expect(nickname).not.toHaveAttribute("aria-invalid", "true");
    expect(
      screen.getAllByRole("button", { name: "Close notification" }),
    ).toHaveLength(1);
    await user.clear(id);
    await user.click(within(form).getByRole("button"));
    await waitFor(() => expect(id).toHaveFocus());
    expect(id).toHaveAttribute("aria-invalid", "true");
    expect(within(form).queryByRole("alert")).not.toBeInTheDocument();
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(nickname).toHaveValue("Preserved");
  });

  it("accepts a real empty logout response", async () => {
    const fetch = vi
      .fn<typeof globalThis.fetch>()
      .mockResolvedValue(new Response(null, { status: 204 }));
    vi.stubGlobal("fetch", fetch);
    mountForms();
    await userEvent
      .setup()
      .click(screen.getByRole("button", { name: "Log out test session" }));
    expect(
      await screen.findByText("Logout completed with no response body."),
    ).toBeVisible();
    expect(fetch).toHaveBeenCalledTimes(1);
    expect(
      screen.queryByRole("button", { name: "Close notification" }),
    ).not.toBeInTheDocument();
  });
});

function ErrorClearingForm() {
  const form = useAppForm({
    defaultValues: { nickname: "old", other: "other" },
  });
  return (
    <form.AppForm>
      <form.Form label="Error clearing">
        <form.AppField
          name="nickname"
          validators={{ onChange: () => "Client validation remains" }}
        >
          {(field) => <field.FormField label="Nickname" />}
        </form.AppField>
        <form.AppField name="other">
          {(field) => <field.FormField label="Other" />}
        </form.AppField>
        <button
          type="button"
          onClick={() => {
            const server = new ServerFormError(
              new ApiError({ kind: "http", code: "invalid_nickname" }),
            );
            form.setFieldMeta("nickname", (meta) => ({
              ...meta,
              errorMap: { ...meta.errorMap, onSubmit: server },
            }));
            form.setFieldMeta("other", (meta) => ({
              ...meta,
              errorMap: { ...meta.errorMap, onSubmit: server },
            }));
          }}
        >
          Show server errors
        </button>
      </form.Form>
    </form.AppForm>
  );
}

it("clears only the edited field's server error and retains current client validation", async () => {
  const user = userEvent.setup();
  render(
    <I18nProvider i18n={i18n}>
      <ErrorClearingForm />
    </I18nProvider>,
  );
  await user.click(screen.getByRole("button", { name: "Show server errors" }));
  const nickname = screen.getByRole("textbox", { name: "Nickname" });
  const other = screen.getByRole("textbox", { name: "Other" });
  expect(nickname).toHaveAttribute("aria-invalid", "true");
  expect(other).toHaveAttribute("aria-invalid", "true");
  await user.type(nickname, " edited");
  expect(nickname).toHaveAccessibleDescription("Client validation remains");
  expect(other).toHaveAttribute("aria-invalid", "true");
  expect(other).toHaveAccessibleDescription();
});
