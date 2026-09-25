import { setupI18n } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { act, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, expect, it, vi } from "vitest";
import { ApiError } from "@/api/errors";
import {
  AppNotifications,
  notificationQueue,
  notifyError,
  notifySuccess,
} from "@/components/custom/AppNotifications";

it("keeps independent errors visible and translates queued descriptions when locale changes", () => {
  const i18n = setupI18n({
    locale: "en",
    messages: {
      en: {
        "error.no_session": "Sign in for this request",
        "error.sudo_required": "Verify this action",
        "notification.region": "Notifications",
        "notification.close": "Close notification",
      },
      zh: {
        "error.no_session": "请先登录",
        "error.sudo_required": "请验证此次操作",
        "notification.region": "通知",
        "notification.close": "关闭通知",
      },
    },
  });
  render(
    <I18nProvider i18n={i18n}>
      <AppNotifications />
    </I18nProvider>,
  );

  act(() => {
    notifyError(
      new ApiError({ kind: "http", status: 401, code: "no_session" }),
    );
    notifyError(
      new ApiError({ kind: "http", status: 401, code: "sudo_required" }),
    );
    notifyError(new DOMException("Cancelled", "AbortError"));
  });
  expect(screen.getByText("Sign in for this request")).toBeVisible();
  expect(screen.getByText("Verify this action")).toBeVisible();
  expect(
    screen.getAllByRole("button", { name: "Close notification" }),
  ).toHaveLength(2);

  act(() => i18n.activate("zh"));
  expect(screen.getByText("请先登录")).toBeVisible();
  expect(screen.getByText("请验证此次操作")).toBeVisible();
  expect(
    screen.queryByText("Sign in for this request"),
  ).not.toBeInTheDocument();
});

it("shows a success as a success toast and follows a change of language", () => {
  const i18n = setupI18n({
    locale: "en",
    messages: {
      en: {
        "success.passkey.added": "Passkey added",
        "notification.region": "Notifications",
        "notification.close": "Close notification",
      },
      zh: {
        "success.passkey.added": "通行密钥已添加",
        "notification.region": "通知",
        "notification.close": "关闭通知",
      },
    },
  });
  render(
    <I18nProvider i18n={i18n}>
      <AppNotifications />
    </I18nProvider>,
  );

  act(() => notifySuccess({ id: "success.passkey.added" }));
  const title = screen.getByText("Passkey added");
  expect(title).toBeVisible();
  expect(title.closest("[data-slot='toast']")).toHaveClass("toast--success");

  act(() => i18n.activate("zh"));
  expect(screen.getByText("通行密钥已添加")).toBeVisible();
});

const detailsMessages = {
  "error.no_session": "Sign in for this request",
  "error.network": "Could not connect",
  "error.rate_limited": "Too many requests",
  "error.invalid_response": "Invalid response",
  "notification.region": "Notifications",
  "notification.close": "Close notification",
  "notification.details": "Details",
  "notification.request_id": "Request ID: {0}",
  "request_details.title": "Request details",
  "request_details.request": "Request",
  "request_details.status": "Status",
  "request_details.no_response": "No response was received from the server.",
  "request_details.request_body": "Request body",
  "request_details.response_headers": "Response headers",
  "request_details.response_body": "Response body",
  "request_details.close": "Close",
};

function renderNotifications() {
  const i18n = setupI18n({ locale: "en", messages: { en: detailsMessages } });
  render(
    <I18nProvider i18n={i18n}>
      <AppNotifications />
    </I18nProvider>,
  );
}

// The queue outlives the provider. A test that pressed a toast leaves the
// queue paused as if the pointer were still on it, and a closing toast holds
// its visible slot until its exit ends, so both are settled here.
afterEach(async () => {
  vi.useFakeTimers();
  act(() => {
    notificationQueue.resumeAll();
    notificationQueue.clear();
  });
  await act(() => vi.runAllTimersAsync());
  vi.useRealTimers();
});

it("opens the request details from an error toast and closes them again", async () => {
  const user = userEvent.setup();
  renderNotifications();

  act(() =>
    notifyError(
      new ApiError({
        kind: "http",
        status: 401,
        code: "no_session",
        requestId: "request-1",
        exchange: {
          method: "POST",
          path: "/api/prohibitorum/me/password/set",
          requestBody: '{\n  "password": "••••••"\n}',
          response: {
            status: 401,
            headers: [
              ["content-type", "application/json"],
              ["x-request-id", "request-1"],
            ],
            body: '{\n  "code": "no_session"\n}',
          },
        },
      }),
    ),
  );
  await user.click(screen.getByRole("button", { name: "Details" }));

  const dialog = await screen.findByRole("dialog", { name: "Request details" });
  expect(
    within(dialog).getByText("POST /api/prohibitorum/me/password/set"),
  ).toBeVisible();
  expect(within(dialog).getByText("401")).toBeVisible();
  expect(within(dialog).getByLabelText("Request body")).toHaveValue(
    '{\n  "password": "••••••"\n}',
  );
  expect(within(dialog).getByLabelText("Response headers")).toHaveValue(
    "content-type: application/json\nx-request-id: request-1",
  );
  expect(within(dialog).getByLabelText("Response body")).toHaveValue(
    '{\n  "code": "no_session"\n}',
  );
  expect(
    within(dialog).queryByText("No response was received from the server."),
  ).not.toBeInTheDocument();

  await user.click(within(dialog).getByRole("button", { name: "Close" }));
  await waitFor(() =>
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument(),
  );
});

it("offers no details for an error that carries no request", () => {
  renderNotifications();

  act(() => notifyError(new ApiError({ kind: "invalid-response" })));

  expect(screen.getByText("Invalid response")).toBeVisible();
  expect(
    screen.queryByRole("button", { name: "Details" }),
  ).not.toBeInTheDocument();
});

it("shows only the request of a network failure", async () => {
  const user = userEvent.setup();
  renderNotifications();

  act(() =>
    notifyError(
      new ApiError({
        kind: "network",
        exchange: { method: "GET", path: "/api/prohibitorum/me" },
      }),
    ),
  );
  await user.click(screen.getByRole("button", { name: "Details" }));

  const dialog = await screen.findByRole("dialog", { name: "Request details" });
  expect(within(dialog).getByText("GET /api/prohibitorum/me")).toBeVisible();
  expect(
    within(dialog).getByText("No response was received from the server."),
  ).toBeVisible();
  expect(within(dialog).queryByText("Status")).not.toBeInTheDocument();
  expect(
    within(dialog).queryByLabelText("Response headers"),
  ).not.toBeInTheDocument();
  expect(
    within(dialog).queryByLabelText("Response body"),
  ).not.toBeInTheDocument();
});

it("closes an error on its own", async () => {
  vi.useFakeTimers();
  renderNotifications();

  act(() =>
    notifyError(
      new ApiError({ kind: "http", status: 401, code: "rate_limited" }),
    ),
  );
  expect(screen.getByText("Too many requests")).toBeVisible();

  await act(() => vi.advanceTimersByTimeAsync(10_000));
  expect(screen.queryByText("Too many requests")).not.toBeInTheDocument();
});
