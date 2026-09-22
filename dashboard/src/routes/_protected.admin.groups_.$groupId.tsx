import { createFileRoute } from "@tanstack/react-router";
import { AdminGroupForm } from "@/pages/admin/AdminGroupForm";

export const Route = createFileRoute("/_protected/admin/groups_/$groupId")({
  component: RouteComponent,
});

function RouteComponent() {
  const { groupId } = Route.useParams();
  return <AdminGroupForm groupId={Number(groupId)} />;
}
