import type { PublicConfig } from "@/api/raw-paths";
import { defaultLoginAppearance } from "@/components/custom/login-appearance/appearance";

/** What `GET /config` reports for an instance with nothing customised. */
export function publicConfigFixture(
  overrides: Partial<PublicConfig> = {},
): PublicConfig {
  return {
    instanceName: "Prohibitorum",
    hasCustomIcon: false,
    iconUrl: "",
    iconEtag: "",
    maintenanceMode: false,
    maintenanceMessages: {},
    maintenanceDefaultLocale: "en",
    loginAppearance: defaultLoginAppearance,
    loginImages: [],
    totp: { issuer: "Prohibitorum", algorithm: "SHA1", digits: 6, period: 30 },
    ...overrides,
  };
}
