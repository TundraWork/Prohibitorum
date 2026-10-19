import type { components } from "@/api/generated/schema";

export type SamlApp = components["schemas"]["SAMLApplicationView"];

/** A signing certificate that is still good for years, so no test depends on today's date. */
export const farFutureExpiry = "2031-03-15T12:00:00Z";

/** An enabled, open SAML application with one endpoint and one valid certificate. */
export function samlAppFixture(overrides: Partial<SamlApp> = {}): SamlApp {
  return {
    id: 7,
    entityId: "https://wiki.example.test/saml/metadata",
    displayName: "Company wiki",
    nameIdFormat: "urn:oasis:names:tc:SAML:2.0:nameid-format:persistent",
    attributeMap: [
      {
        name: "mail",
        name_format: "urn:oasis:names:tc:SAML:2.0:attrname-format:basic",
        friendly_name: "Mail address",
        source: "attributes.mail",
        multi: false,
      },
    ],
    requireSignedAuthnRequest: false,
    allowIdpInitiated: false,
    disabled: false,
    accessRestricted: false,
    acs: [
      {
        binding: "urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST",
        location: "https://wiki.example.test/saml/acs",
        index: 0,
        isDefault: true,
      },
    ],
    keys: [{ use: "signing", notAfter: farFutureExpiry }],
    createdAt: "2026-09-01T00:00:00Z",
    ...overrides,
  };
}
