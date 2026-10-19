import { createFileRoute } from "@tanstack/react-router";
import { AdminGroupNew } from "@/pages/admin/groups/AdminGroupNew";

export const Route = createFileRoute("/_protected/admin/_admin/groups/new")({
  component: () => <AdminGroupNew />,
});
