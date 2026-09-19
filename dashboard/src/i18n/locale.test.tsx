import { act, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

let stopLocaleSync: (() => void) | undefined;

async function mountApplication() {
  const [
    { App },
    { i18n, initializeLocale, store },
    { Provider },
    { I18nProvider },
  ] = await Promise.all([
    import("@/App"),
    import("@/i18n"),
    import("jotai"),
    import("@lingui/react"),
  ]);
  stopLocaleSync = initializeLocale();
  const localeBeforeMount = document.documentElement.lang;
  const view = render(
    <Provider store={store}>
      <I18nProvider i18n={i18n}>
        <App />
      </I18nProvider>
    </Provider>,
  );
  await screen.findByRole("heading", { level: 1 });
  return { ...view, localeBeforeMount };
}

beforeEach(() => {
  vi.resetModules();
  localStorage.clear();
  window.history.replaceState(
    null,
    "",
    "/preview/components?lang=zh&returnTo=%2Fapps#details",
  );
});

afterEach(() => {
  stopLocaleSync?.();
  stopLocaleSync = undefined;
});

describe("language preference", () => {
  it("uses English without a preference, independent of URL and old storage", async () => {
    localStorage.setItem("locale", "zh");
    localStorage.setItem("language", "zh");
    const { localeBeforeMount } = await mountApplication();

    expect(localeBeforeMount).toBe("en");
    expect(
      screen.getByRole("heading", { name: "Interface preview" }),
    ).toBeVisible();
    expect(screen.getByRole("button", { name: /Language/ })).toBeVisible();
  });

  it("keeps input and URL when switching and activates the saved language before remount", async () => {
    const user = userEvent.setup();
    const view = await mountApplication();
    const originalUrl = window.location.href;
    const name = screen.getByRole("textbox", { name: "Display name" });
    await user.clear(name);
    await user.type(name, "Unsubmitted edit");
    await user.click(screen.getByRole("button", { name: /Language/ }));
    await user.click(screen.getByRole("menuitemradio", { name: "中文" }));

    expect(screen.getByRole("heading", { name: "界面预览" })).toBeVisible();
    expect(screen.getByRole("textbox", { name: "显示名称" })).toHaveValue(
      "Unsubmitted edit",
    );
    expect(screen.getByRole("button", { name: "显示通知" })).toBeVisible();
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(window.location.href).toBe(originalUrl);

    view.unmount();
    stopLocaleSync?.();
    vi.resetModules();
    const reloaded = await mountApplication();
    expect(reloaded.localeBeforeMount).toBe("zh-CN");
    expect(screen.getByRole("heading", { name: "界面预览" })).toBeVisible();
    expect(window.location.href).toBe(originalUrl);
  });

  it("updates content, accessible names, and an open notification from another tab without losing input", async () => {
    const user = userEvent.setup();
    await mountApplication();
    const originalUrl = window.location.href;
    const name = screen.getByRole("textbox", { name: "Display name" });
    await user.clear(name);
    await user.type(name, "Still editing");
    await user.click(screen.getByRole("button", { name: "Show notification" }));
    expect(
      screen.getByText("Preview notification. No data was submitted."),
    ).toBeVisible();

    act(() => {
      localStorage.setItem("prohibitorum.locale", '"zh"');
      window.dispatchEvent(
        new StorageEvent("storage", {
          key: "prohibitorum.locale",
          newValue: '"zh"',
          storageArea: localStorage,
        }),
      );
    });

    expect(screen.getByRole("heading", { name: "界面预览" })).toBeVisible();
    expect(screen.getByRole("textbox", { name: "显示名称" })).toHaveValue(
      "Still editing",
    );
    expect(screen.getByText("这是预览通知，没有提交任何数据。")).toBeVisible();
    expect(screen.getByRole("button", { name: "关闭通知" })).toBeVisible();
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(window.location.href).toBe(originalUrl);

    await user.click(screen.getByRole("button", { name: "关闭通知" }));
    await waitFor(() => {
      expect(
        screen.queryByText("这是预览通知，没有提交任何数据。"),
      ).not.toBeInTheDocument();
    });
  });

  it("activates English for a stored value other than the exact supported Chinese value", async () => {
    localStorage.setItem("prohibitorum.locale", '" zh "');
    const { localeBeforeMount } = await mountApplication();

    expect(localeBeforeMount).toBe("en");
    expect(
      screen.getByRole("heading", { name: "Interface preview" }),
    ).toBeVisible();
  });
});
