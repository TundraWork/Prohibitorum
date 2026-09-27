import { createFileRoute } from "@tanstack/react-router";
import { PublicLayout } from "@/components/custom/PublicLayout";
import {
  AppRouteError,
  PublicPending,
  PublicRouteNotFound,
} from "@/components/custom/RouteFeedback";

export const Route = createFileRoute("/_public")({
  component: PublicLayout,
  pendingComponent: PublicPending,
  errorComponent: AppRouteError,
  notFoundComponent: PublicRouteNotFound,
});
