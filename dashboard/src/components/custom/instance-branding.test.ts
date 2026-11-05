import { describe, expect, it } from "vitest";
import type { PublicConfig } from "@/api/raw-paths";
import {
  downloadFilename,
  instanceBranding,
  versionedUrl,
} from "@/components/custom/instance-branding";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";

const config: PublicConfig = {
  instanceName: "Home",
  hasCustomIcon: true,
  iconUrl: "/branding/icon",
  iconEtag: 'W/"abc"',
  maintenanceMode: false,
  maintenanceMessages: {},
  maintenanceDefaultLocale: "en",
  loginAppearance: defaultLoginAppearance,
  loginImages: [],
  totp: { issuer: "Home", algorithm: "SHA1", digits: 6, period: 30 },
};

describe("instance branding", () => {
  it("versions the icon by its ETag, so a new icon is a new URL", () => {
    expect(instanceBranding(config)).toEqual({
      name: "Home",
      iconUrl: `/branding/icon?v=${encodeURIComponent('W/"abc"')}`,
      loginAppearance: defaultLoginAppearance,
      loginImages: [],
    });
  });

  it("versions each sign-in image by its ETag and keeps their order", () => {
    expect(
      instanceBranding({
        ...config,
        loginImages: [
          { id: 7, url: "/branding/login-images/7", etag: "e7" },
          { id: 3, url: "/branding/login-images/3", etag: "e3" },
        ],
      }).loginImages,
    ).toEqual([
      { id: 7, url: "/branding/login-images/7?v=e7" },
      { id: 3, url: "/branding/login-images/3?v=e3" },
    ]);
  });

  it("leaves a URL without an ETag, or one that carries its own content, as it is", () => {
    expect(versionedUrl("/branding/icon", "")).toBe("/branding/icon");
    expect(versionedUrl("data:image/svg+xml;utf8,x", "e1")).toBe(
      "data:image/svg+xml;utf8,x",
    );
    expect(versionedUrl("/branding/icon?size=64", "e1")).toBe(
      "/branding/icon?size=64&v=e1",
    );
  });
});

describe("download file names", () => {
  it("lead with the instance name", () => {
    expect(downloadFilename("Home", "recovery-codes.txt")).toBe(
      "Home-recovery-codes.txt",
    );
    expect(downloadFilename("示例实例", "access-token.txt")).toBe(
      "示例实例-access-token.txt",
    );
  });

  it("replace what a file name cannot hold, a run at a time", () => {
    expect(downloadFilename("Yet Another Product", "invitation.txt")).toBe(
      "Yet-Another-Product-invitation.txt",
    );
    expect(downloadFilename('A/B\\C: "D" <E>|F?*\n\tG', "x.txt")).toBe(
      "A-B-C-D-E-F-G-x.txt",
    );
  });

  it("drop leading and trailing dots and hyphens", () => {
    expect(downloadFilename("..hidden.", "x.txt")).toBe("hidden-x.txt");
    expect(downloadFilename(" Home ", "x.txt")).toBe("Home-x.txt");
  });

  it("fall back to the product name when nothing usable is left", () => {
    expect(downloadFilename("", "x.txt")).toBe("Prohibitorum-x.txt");
    expect(downloadFilename("///", "x.txt")).toBe("Prohibitorum-x.txt");
  });
});
