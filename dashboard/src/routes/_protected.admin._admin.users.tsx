import { createFileRoute } from "@tanstack/react-router";
import { AdminUsers } from "@/pages/admin/AdminUsers";
import { userFilters } from "@/pages/admin/user-filters";

export const Route = createFileRoute("/_protected/admin/users")({
  validateSearch: userFilters,
  component: AdminUsers,
});
