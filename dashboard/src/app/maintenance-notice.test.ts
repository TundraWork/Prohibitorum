import { describe, expect, it } from "vitest";
import type { PublicConfig } from "@/api/raw-paths";
import { pickMaintenanceNotice } from "@/app/maintenance-notice";
import { publicConfigFixture } from "@/test/public-config";

function config(
  messages: PublicConfig["maintenanceMessages"],
  defaultLocale = "en",
): PublicConfig {
  return publicConfigFixture({
    maintenanceMessages: messages,
    maintenanceDefaultLocale: defaultLocale,
  });
}

describe("pickMaintenanceNotice", () => {
  it("uses the visitor's language when it has a notice", () => {
    expect(
      pickMaintenanceNotice(
        config({ en: "Back at 17:00 UTC", zh: "17:00 UTC 恢复" }),
        "zh",
      ),
    ).toEqual({ text: "17:00 UTC 恢复", locale: "zh" });
  });

  it("falls back to the default language when the visitor's has none", () => {
    expect(
      pickMaintenanceNotice(config({ en: "Back at 17:00 UTC" }, "en"), "zh"),
    ).toEqual({ text: "Back at 17:00 UTC", locale: "en" });
  });

  it("returns null when no language has a notice", () => {
    expect(pickMaintenanceNotice(config({}), "en")).toBeNull();
  });

  it("still serves the default language's notice to its own visitors", () => {
    expect(pickMaintenanceNotice(config({ zh: "维护中" }, "zh"), "zh")).toEqual(
      { text: "维护中", locale: "zh" },
    );
  });

  it("ignores a default language outside the known locales", () => {
    expect(pickMaintenanceNotice(config({ en: "Hi" }, "fr"), "zh")).toBeNull();
  });
});
