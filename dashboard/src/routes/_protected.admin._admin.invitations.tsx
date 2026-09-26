import { createFileRoute } from "@tanstack/react-router";
import { AdminInvitations } from "@/pages/admin/AdminInvitations";

export const Route = createFileRoute("/_protected/admin/_admin/invitations")({
  component: AdminInvitations,
});
