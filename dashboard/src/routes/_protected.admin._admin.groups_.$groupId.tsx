import { useSuspenseQuery } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { groupQueryOptions } from "@/api/queries";
import { AdminGroupForm } from "@/pages/admin/AdminGroupForm";

export const Route = createFileRoute("/_protected/admin/groups_/$groupId")({
  loader: ({ context: { queryClient }, params: { groupId } }) =>
    queryClient.ensureQueryData(groupQueryOptions(Number(groupId))),
  component: RouteComponent,
});

function RouteComponent() {
  const { groupId } = Route.useParams();
  const { data: group } = useSuspenseQuery(groupQueryOptions(Number(groupId)));
  return <AdminGroupForm group={group} />;
}
