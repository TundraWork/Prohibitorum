import { createFileRoute } from "@tanstack/react-router";
import { AdminInvitationForm } from "@/pages/admin/AdminInvitationForm";

export const Route = createFileRoute(
  "/_protected/admin/_admin/invitations_/new",
)({
  component: AdminInvitationForm,
});
