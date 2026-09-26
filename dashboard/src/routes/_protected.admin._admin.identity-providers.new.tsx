import { createFileRoute } from "@tanstack/react-router";
import { AdminIdentityProviderNew } from "@/pages/admin/identity-providers/AdminIdentityProviderNew";

export const Route = createFileRoute(
  "/_protected/admin/_admin/identity-providers/new",
)({
  component: AdminIdentityProviderNew,
});
