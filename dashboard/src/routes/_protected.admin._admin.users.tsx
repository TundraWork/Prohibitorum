import { createFileRoute } from "@tanstack/react-router";
import { AdminUsers } from "@/pages/admin/users/AdminUsers";
import { userSearch } from "@/pages/admin/users/user-filters";

export const Route = createFileRoute("/_protected/admin/_admin/users")({
  validateSearch: userSearch,
  component: AdminUsers,
});
