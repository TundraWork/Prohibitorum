import { createFileRoute } from "@tanstack/react-router";
import { AdminGroups } from "@/pages/admin/AdminGroups";

export const Route = createFileRoute("/_protected/admin/groups/")({
  component: AdminGroups,
});
