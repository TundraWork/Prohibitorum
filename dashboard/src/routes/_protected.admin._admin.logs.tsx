import { createFileRoute } from "@tanstack/react-router";
import { AdminLogs } from "@/pages/admin/AdminLogs";
import { auditSearch } from "@/pages/admin/audit-filters";

export const Route = createFileRoute("/_protected/admin/logs")({
  validateSearch: auditSearch,
  component: AdminLogs,
});
