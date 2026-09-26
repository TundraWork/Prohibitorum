import { createFileRoute } from "@tanstack/react-router";
import { AdminIdentityProviders } from "@/pages/admin/identity-providers/AdminIdentityProviders";

export const Route = createFileRoute(
  "/_protected/admin/_admin/identity-providers/",
)({
  component: AdminIdentityProviders,
});
