import { createFileRoute } from "@tanstack/react-router";
import { AdminUsers } from "@/pages/admin/AdminUsers";
import { userSearch } from "@/pages/admin/user-filters";

export const Route = createFileRoute("/_protected/admin/_admin/users")({
  validateSearch: userSearch,
  component: AdminUsers,
});
