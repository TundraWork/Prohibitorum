import { describe, expect, it } from "vitest";
import type { PublicConfig } from "@/api/raw-paths";
import {
  instanceBranding,
  versionedUrl,
} from "@/components/custom/instance-branding";

const config: PublicConfig = {
  instanceName: "Home",
  hasCustomIcon: true,
  iconUrl: "/branding/icon",
  iconEtag: 'W/"abc"',
  maintenanceMode: false,
  maintenanceMessage: "",
  hasCustomBackground: false,
  backgroundUrl: "/branding/background",
  backgroundEtag: "",
  totp: { issuer: "Home", algorithm: "SHA1", digits: 6, period: 30 },
};

describe("instance branding", () => {
  it("versions the icon by its ETag, so a new icon is a new URL", () => {
    expect(instanceBranding(config)).toEqual({
      name: "Home",
      iconUrl: `/branding/icon?v=${encodeURIComponent('W/"abc"')}`,
    });
  });

  it("offers a background only once one has been uploaded", () => {
    expect(
      instanceBranding({
        ...config,
        hasCustomBackground: true,
        backgroundEtag: "bg1",
      }).backgroundUrl,
    ).toBe("/branding/background?v=bg1");
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
