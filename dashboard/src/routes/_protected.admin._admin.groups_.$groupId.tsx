import { createFileRoute } from "@tanstack/react-router";
import { groupQueryOptions } from "@/api/queries";
import { AdminGroup } from "@/pages/admin/groups/AdminGroup";

export const Route = createFileRoute(
  "/_protected/admin/_admin/groups_/$groupId",
)({
  loader: ({ context: { queryClient }, params: { groupId } }) =>
    queryClient.ensureQueryData(groupQueryOptions(Number(groupId))),
  component: RouteComponent,
});

function RouteComponent() {
  const { groupId } = Route.useParams();
  return <AdminGroup groupId={Number(groupId)} />;
}
