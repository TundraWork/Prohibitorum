import { createFileRoute } from "@tanstack/react-router";
import { PublicLayout } from "@/components/custom/PublicLayout";
import { PublicPending } from "@/components/custom/RouteFeedback";

export const Route = createFileRoute("/_public")({
  component: PublicLayout,
  pendingComponent: PublicPending,
});
