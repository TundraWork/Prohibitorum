import { createFileRoute } from "@tanstack/react-router";
import { AdminGroupForm } from "@/pages/admin/AdminGroupForm";

export const Route = createFileRoute("/_protected/admin/groups/new")({
  component: () => <AdminGroupForm />,
});
