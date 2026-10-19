import type { components } from "@/api/generated/schema";

export type ForwardAuthApp = components["schemas"]["ForwardAuthAppView"];

/** An enabled, open forward-auth application identifying people by username. */
export function forwardAuthAppFixture(
  overrides: Partial<ForwardAuthApp> = {},
): ForwardAuthApp {
  return {
    clientId: "grafana",
    displayName: "Grafana",
    forwardAuthHost: "grafana.example.test",
    remoteUserSource: "username",
    disabled: false,
    accessRestricted: false,
    createdAt: "2026-09-01T00:00:00Z",
    ...overrides,
  };
}
