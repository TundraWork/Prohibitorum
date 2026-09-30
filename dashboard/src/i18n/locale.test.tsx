import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";

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

/**
 * The sign-in page is the one every visitor reaches. Before it paints the app
 * reads the instance's branding, whether the instance is set up, and whether
 * anyone is signed in; nothing else here reaches the server.
 */
const config = {
  instanceName: "Prohibitorum",
  hasCustomIcon: false,
  iconUrl: "/branding/icon",
  iconEtag: "",
  maintenanceMode: false,
  maintenanceMessage: "",
  loginAppearance: defaultLoginAppearance,
  loginImages: [],
  totp: { issuer: "Prohibitorum", algorithm: "SHA1", digits: 6, period: 30 },
};

beforeEach(() => {
  vi.resetModules();
  vi.stubGlobal(
    "fetch",
    vi.fn(async (request: Request) => {
      switch (new URL(request.url).pathname) {
        case "/api/prohibitorum/config":
          return Response.json(config);
        case "/api/prohibitorum/auth/status":
          return Response.json({ bootstrapped: true });
        case "/api/prohibitorum/auth/federation":
          return Response.json([]);
        case "/api/prohibitorum/me":
          return Response.json(
            { code: "no_session", requestId: "locale-test" },
            { status: 401 },
          );
        default:
          return new Response(null, { status: 404 });
      }
    }),
  );
  localStorage.clear();
  window.history.replaceState(
    null,
    "",
    "/login?lang=zh&return_to=%2Fapps#details",
  );
});

afterEach(() => {
  stopLocaleSync?.();
  stopLocaleSync = undefined;
  vi.unstubAllGlobals();
});

describe("language preference", () => {
  it("uses English without a preference, independent of URL and old storage", async () => {
    localStorage.setItem("locale", "zh");
    localStorage.setItem("language", "zh");
    const { localeBeforeMount } = await mountApplication();

    expect(localeBeforeMount).toBe("en");
    expect(screen.getByRole("heading", { name: "Sign in" })).toBeVisible();
    expect(screen.getByRole("button", { name: /Language/ })).toBeVisible();
  });

  it("keeps input and URL when switching and activates the saved language before remount", async () => {
    const user = userEvent.setup();
    const view = await mountApplication();
    const originalUrl = window.location.href;
    const name = screen.getByRole("textbox", { name: "Username" });
    await user.type(name, "Unsubmitted edit");
    await user.click(screen.getByRole("button", { name: /Language/ }));
    await user.click(screen.getByRole("menuitemradio", { name: "中文" }));

    expect(screen.getByRole("heading", { name: "登录" })).toBeVisible();
    expect(screen.getByRole("textbox", { name: "用户名" })).toHaveValue(
      "Unsubmitted edit",
    );
    expect(
      screen.getByRole("button", { name: "使用通行密钥登录" }),
    ).toBeVisible();
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(window.location.href).toBe(originalUrl);

    view.unmount();
    stopLocaleSync?.();
    vi.resetModules();
    const reloaded = await mountApplication();
    expect(reloaded.localeBeforeMount).toBe("zh-CN");
    expect(screen.getByRole("heading", { name: "登录" })).toBeVisible();
    expect(window.location.href).toBe(originalUrl);
  });

  it("updates content and accessible names from another tab without losing input", async () => {
    const user = userEvent.setup();
    await mountApplication();
    const originalUrl = window.location.href;
    const name = screen.getByRole("textbox", { name: "Username" });
    await user.type(name, "Still editing");

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

    expect(screen.getByRole("heading", { name: "登录" })).toBeVisible();
    expect(screen.getByRole("textbox", { name: "用户名" })).toHaveValue(
      "Still editing",
    );
    expect(screen.getByRole("button", { name: "使用密码继续" })).toBeVisible();
    expect(document.documentElement.lang).toBe("zh-CN");
    expect(window.location.href).toBe(originalUrl);
  });

  it("activates English for a stored value other than the exact supported Chinese value", async () => {
    localStorage.setItem("prohibitorum.locale", '" zh "');
    const { localeBeforeMount } = await mountApplication();

    expect(localeBeforeMount).toBe("en");
    expect(screen.getByRole("heading", { name: "Sign in" })).toBeVisible();
  });
});
