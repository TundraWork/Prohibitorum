import { setupI18n } from "@lingui/core";
import { I18nProvider } from "@lingui/react";
import { act, render, screen } from "@testing-library/react";
import { expect, it } from "vitest";
import { ApiError } from "@/api/errors";
import {
  AppNotifications,
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
